package sshdial

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"

	// skeema/knownhosts 是 x/crypto/ssh/knownhosts 的薄包装。裸用 x/crypto
	// 有个著名的坑：它只校验不参与算法协商，服务器多半有 rsa/ecdsa/ed25519
	// 好几把 host key，握手挑中的那把只要不是 known_hosts 里存的类型就报
	// key mismatch——「手工 ssh 连得上、程序连不上」就是它。skeema 版能按
	// known_hosts 已有条目给出 HostKeyAlgorithms，让协商去挑我们认得的 key。
	"github.com/skeema/knownhosts"
	"golang.org/x/crypto/ssh"
)

// knownHostsMu 罩住 known_hosts 的追加写：并发拨多条连接时首连补录
// 不能交错写同一文件。
var knownHostsMu sync.Mutex

// HostKeyCallback 用 ~/.ssh/known_hosts 校验对端指纹，target 是
// host:port 形式的地址（决定查哪条记录与收窄哪些算法）。
func HostKeyCallback(target string) (ssh.HostKeyCallback, []string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, fmt.Errorf("定位家目录失败: %w", err)
	}
	return hostKeyVerifier(filepath.Join(home, ".ssh", "known_hosts"), target)
}

// hostKeyVerifier 基于指定 known_hosts 文件构造校验回调与算法偏好。
//
// 校验策略等价 OpenSSH 的 `StrictHostKeyChecking=accept-new`：
//   - 没连过的主机：放行并把指纹自动补录进 known_hosts（省掉手工
//     ssh-keyscan 的一次性成本，「测试连接」按钮就是补录入口）；
//   - 连过且指纹一致：放行；
//   - 连过但指纹**变了**：拒绝。这是唯一真正的中间人信号，刻意不提供
//     跳过开关——这条连接后面挂着的是生产环境。
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
		return nil, nil, fmt.Errorf("%w: 读 %s 失败: %v", ErrInvalid, path, err)
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
			slog.Info("known_hosts 补录主机指纹", "host", hostname, "type", key.Type())
			return nil
		case knownhosts.IsHostKeyChanged(err):
			return fmt.Errorf("主机指纹与 known_hosts 记录不一致，可能主机重装过、也可能有中间人。人工核实后删掉 %s 里的旧记录再连: %w", path, err)
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
