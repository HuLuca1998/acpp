package datasource

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/ssh"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
	"acpp/server/internal/sshdial"
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

// dialTunnel 连上跳板机。握手细节（认证方式、known_hosts 校验）全在
// sshdial，这里只负责把拿到的 client 登记进驱动的隧道表。
func dialTunnel(ctx context.Context, srv *model.Server) (*tunnel, error) {
	client, err := sshdial.Dial(ctx, sshdial.Config{
		Host:       srv.Host,
		Port:       srv.Port,
		User:       srv.User,
		Auth:       srv.Auth,
		Password:   srv.Password,
		KeyPath:    srv.KeyPath,
		Passphrase: srv.Passphrase,
		Timeout:    dialTimeout,
	})
	if err != nil {
		// 配置类错误换成本项目的哨兵错误，httpapi 才会映射成 400 而不是 500。
		if errors.Is(err, sshdial.ErrInvalid) {
			return nil, fmt.Errorf("%w: %s", service.ErrInvalid,
				strings.TrimPrefix(err.Error(), sshdial.ErrInvalid.Error()+": "))
		}
		return nil, err
	}

	t := &tunnel{
		id:     strconv.FormatUint(tunnelSeq.Add(1), 36),
		client: client,
	}
	tunnelMu.Lock()
	tunnels[t.id] = t.client
	tunnelMu.Unlock()
	return t, nil
}
