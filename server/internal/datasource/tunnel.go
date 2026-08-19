package datasource

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-sql-driver/mysql"
	// skeema/knownhosts 是 x/crypto/ssh/knownhosts 的薄包装。裸用 x/crypto
	// 有个著名的坑：它只校验不参与算法协商，服务器多半有 rsa/ecdsa/ed25519
	// 好几把 host key，握手挑中的那把只要不是 known_hosts 里存的类型就报
	// key mismatch——「手工 ssh 连得上、程序连不上」就是它。skeema 版能按
	// known_hosts 已有条目给出 HostKeyAlgorithms，让协商去挑我们认得的 key。
	"github.com/skeema/knownhosts"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// tunnelNet 是注册给 mysql 驱动的自定义网络名。驱动的 dialer 注册表是
// 全局且**不可注销**的，所以只注册这一个名字，每条隧道的实际去向放进
// 下面的 tunnels 表，用 DSN 的 addr 字段（`<隧道id>@<host:port>`）索引。
// 这样隧道随连接一起来去，不在驱动层留下任何残留。
const tunnelNet = "acpp-ssh"

var (
	tunnelMu  sync.Mutex
	tunnels   = map[string]*ssh.Client{}
	tunnelSeq atomic.Uint64
)

func init() {
	// 注册失败只会在名字重复时发生（本包只注册一次），忽略即可。
	mysql.RegisterDialContext(tunnelNet, func(ctx context.Context, addr string) (net.Conn, error) {
		id, target, ok := strings.Cut(addr, "@")
		if !ok {
			return nil, fmt.Errorf("bad tunnel addr %q", addr)
		}
		tunnelMu.Lock()
		client := tunnels[id]
		tunnelMu.Unlock()
		if client == nil {
			return nil, fmt.Errorf("ssh 隧道已关闭")
		}
		return client.DialContext(ctx, "tcp", target)
	})
}

// tunnel 是一条一次性的 SSH 隧道：连接用完即拆，不做复用池——
// 复用池要处理探活、重连与并发争用，而数据库面板的调用频率根本不值得。
type tunnel struct {
	id     string
	client *ssh.Client
}

func (t *tunnel) Close() {
	if t == nil {
		return
	}
	tunnelMu.Lock()
	delete(tunnels, t.id)
	tunnelMu.Unlock()
	_ = t.client.Close()
}

// addr 是这条隧道在 DSN 里的地址写法。
func (t *tunnel) addr(host string, port int) string {
	return t.id + "@" + net.JoinHostPort(host, strconv.Itoa(port))
}

// dialTunnel 连上跳板机，认证手段由数据源的验证方式决定（见 sshAuths）。
func dialTunnel(ctx context.Context, src *model.DataSource) (*tunnel, error) {
	host, user := strings.TrimSpace(src.SSHHost), strings.TrimSpace(src.SSHUser)
	if host == "" {
		return nil, fmt.Errorf("%w: ssh 主机不能为空", service.ErrInvalid)
	}
	if user == "" {
		return nil, fmt.Errorf("%w: ssh 用户不能为空", service.ErrInvalid)
	}
	port := src.SSHPort
	if port <= 0 {
		port = 22
	}

	auths, err := sshAuths(src)
	if err != nil {
		return nil, err
	}
	if len(auths) == 0 {
		return nil, fmt.Errorf("%w: ssh 没有可用的认证方式（私钥、密码、ssh-agent 至少配一个）", service.ErrInvalid)
	}

	target := net.JoinHostPort(host, strconv.Itoa(port))
	hostKey, hostKeyAlgos, err := hostKeyCallback(target)
	if err != nil {
		return nil, err
	}
	// 先用带 ctx 的拨号拿 TCP 连接，再做 SSH 握手——ssh.Dial 自己不接受
	// context，握手阶段卡住的话没有别的办法中断。
	var d net.Dialer
	raw, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return nil, fmt.Errorf("连接跳板机 %s 失败: %w", target, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(deadline)
	}

	conn, chans, reqs, err := ssh.NewClientConn(raw, target, &ssh.ClientConfig{
		User: user,
		Auth: auths,
		// 主机已在 known_hosts 时把算法偏好收窄到我们存过的类型，
		// 服务器才不会递一把我们没见过的 key（见 import 处的坑注）。
		// 未知主机时为空，走默认偏好。
		HostKeyAlgorithms: hostKeyAlgos,
		HostKeyCallback:   hostKey,
		Timeout:           dialTimeout,
	})
	if err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("ssh 握手失败: %w", err)
	}
	_ = raw.SetDeadline(time.Time{})

	t := &tunnel{
		id:     strconv.FormatUint(tunnelSeq.Add(1), 36),
		client: ssh.NewClient(conn, chans, reqs),
	}
	tunnelMu.Lock()
	tunnels[t.id] = t.client
	tunnelMu.Unlock()
	return t, nil
}

// sshAuths 按配置的验证方式组装认证手段。
//
// 公钥档下私钥路径可以留空——那时走 ssh-agent（本机多半挂着一个），
// 这是把 `ssh` 命令能连上的场景原样搬过来，不逼用户再填一遍路径。
func sshAuths(src *model.DataSource) ([]ssh.AuthMethod, error) {
	var auths []ssh.AuthMethod
	mode := src.SSHAuth
	if mode == "" {
		mode = SSHAuthPassword
	}

	if mode == SSHAuthKey || mode == SSHAuthBoth {
		if path := strings.TrimSpace(src.SSHKeyPath); path != "" {
			raw, err := os.ReadFile(expandHome(path))
			if err != nil {
				return nil, fmt.Errorf("%w: 读私钥失败: %v", service.ErrInvalid, err)
			}
			var signer ssh.Signer
			if src.SSHPassphrase != "" {
				signer, err = ssh.ParsePrivateKeyWithPassphrase(raw, []byte(src.SSHPassphrase))
			} else {
				signer, err = ssh.ParsePrivateKey(raw)
			}
			if err != nil {
				return nil, fmt.Errorf("%w: 解析私钥失败（带密码的私钥要填通行短语）: %v", service.ErrInvalid, err)
			}
			auths = append(auths, ssh.PublicKeys(signer))
		} else if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
			conn, err := net.Dial("unix", sock)
			if err != nil {
				return nil, fmt.Errorf("%w: 未填私钥路径且连不上 ssh-agent: %v", service.ErrInvalid, err)
			}
			auths = append(auths, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
		} else {
			return nil, fmt.Errorf("%w: 公钥验证需要填私钥路径（或让 ssh-agent 跑起来）", service.ErrInvalid)
		}
	}

	if mode == SSHAuthPassword || mode == SSHAuthBoth {
		if src.SSHPassword == "" {
			return nil, fmt.Errorf("%w: 密码验证需要填 ssh 密码", service.ErrInvalid)
		}
		auths = append(auths, ssh.Password(src.SSHPassword))
	}
	return auths, nil
}

// hostKeyCallback 用 ~/.ssh/known_hosts 校验跳板机指纹，target 是
// host:port 形式的跳板机地址（决定查哪条记录与收窄哪些算法）。
func hostKeyCallback(target string) (ssh.HostKeyCallback, []string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, fmt.Errorf("定位家目录失败: %w", err)
	}
	return hostKeyVerifier(filepath.Join(home, ".ssh", "known_hosts"), target)
}

// knownHostsMu 罩住 known_hosts 的追加写：并发拨多条隧道时首连补录
// 不能交错写同一文件。
var knownHostsMu sync.Mutex

// hostKeyVerifier 基于指定 known_hosts 文件构造校验回调与算法偏好。
//
// 校验策略等价 OpenSSH 的 `StrictHostKeyChecking=accept-new`：
//   - 没连过的主机：放行并把指纹自动补录进 known_hosts（省掉手工
//     ssh-keyscan 的一次性成本，「测试连接」按钮就是补录入口）；
//   - 连过且指纹一致：放行；
//   - 连过但指纹**变了**：拒绝。这是唯一真正的中间人信号，刻意不提供
//     跳过开关——这条隧道后面挂着的是生产库。
func hostKeyVerifier(path, target string) (ssh.HostKeyCallback, []string, error) {
	// TOFU 语义下文件不存在不是错误，建空文件让首连补录有处可写。
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, fmt.Errorf("准备 %s 失败: %w", filepath.Dir(path), err)
	}
	touch, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("准备 %s 失败: %w", path, err)
	}
	_ = touch.Close()

	db, err := knownhosts.NewDB(path)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: 读 %s 失败: %v", service.ErrInvalid, path, err)
	}
	verify := db.HostKeyCallback()
	cb := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := verify(hostname, remote, key)
		switch {
		case err == nil:
			return nil
		case knownhosts.IsHostUnknown(err):
			if werr := appendKnownHost(path, hostname, remote, key); werr != nil {
				return fmt.Errorf("首次连接补录指纹到 %s 失败: %w", path, werr)
			}
			slog.Info("known_hosts 补录跳板机指纹", "host", hostname, "type", key.Type())
			return nil
		case knownhosts.IsHostKeyChanged(err):
			return fmt.Errorf("跳板机指纹与 known_hosts 记录不一致，可能主机重装过、也可能有中间人。人工核实后删掉 %s 里的旧记录再连: %w", path, err)
		default:
			return err
		}
	}
	return cb, db.HostKeyAlgorithms(target), nil
}

func appendKnownHost(path, hostname string, remote net.Addr, key ssh.PublicKey) error {
	knownHostsMu.Lock()
	defer knownHostsMu.Unlock()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return knownhosts.WriteKnownHost(f, hostname, remote, key)
}

func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}
