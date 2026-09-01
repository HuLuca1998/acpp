// Package sshdial 负责 SSH 拨号：认证方式组装、known_hosts 指纹校验、
// 连接建立。数据源的隧道（拨到跳板机再连 MySQL）与服务器观察（拨上去
// 跑只读命令）共用它——两处要的握手逻辑一模一样，各抄一份的话
// known_hosts 那个算法协商的坑就得再踩一次。
//
// 叶子包，不 import 项目内其他包：参数类错误用自带的 ErrInvalid 表达，
// 由调用方映射到各自的哨兵错误。
package sshdial

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// 验证方式（照 Navicat 的三选一）。做成显式选择而不是「填了哪个用哪个」：
// 两种凭证都留着、但这次只想用公钥，是很常见的诉求，靠猜实现不了。
const (
	AuthPassword = "password"
	AuthKey      = "key"
	AuthBoth     = "both"
)

// DefaultTimeout 是拨号（含握手）的上限。连不上就该快点说，
// 界面上等三十秒不如三秒给个明确失败。
const DefaultTimeout = 8 * time.Second

// ErrInvalid 标记「配置本身有问题」——缺主机、缺凭证、私钥解析不了这类，
// 与「连不上」区分开：前者要用户去改配置，后者是网络或对端的事。
var ErrInvalid = errors.New("invalid ssh config")

// Config 是一次拨号需要的全部信息。刻意不依赖任何模型类型：
// 数据源的跳板配置与服务器的连接配置都能填进来。
type Config struct {
	Host     string
	Port     int // 0 按 22
	User     string
	Auth     string // password / key / both，空按 password
	Password string
	// KeyPath 支持 `~` 展开。key/both 档下留空表示走 ssh-agent——
	// 那是把 `ssh` 命令能连上的场景原样搬过来，不逼用户再填一遍路径。
	KeyPath    string
	Passphrase string
	// Timeout 为 0 时取 DefaultTimeout。
	Timeout time.Duration
}

// Addr 是 host:port 形式的目标地址（端口留空时补 22）。
func (c Config) Addr() string {
	port := c.Port
	if port <= 0 {
		port = 22
	}
	return net.JoinHostPort(strings.TrimSpace(c.Host), strconv.Itoa(port))
}

// Dial 连上目标机器并完成握手。返回的 client 由调用方负责 Close。
func Dial(ctx context.Context, cfg Config) (*ssh.Client, error) {
	host, user := strings.TrimSpace(cfg.Host), strings.TrimSpace(cfg.User)
	if host == "" {
		return nil, fmt.Errorf("%w: ssh 主机不能为空", ErrInvalid)
	}
	if user == "" {
		return nil, fmt.Errorf("%w: ssh 用户不能为空", ErrInvalid)
	}

	auths, err := authMethods(cfg)
	if err != nil {
		return nil, err
	}
	if len(auths) == 0 {
		return nil, fmt.Errorf("%w: ssh 没有可用的认证方式（私钥、密码、ssh-agent 至少配一个）", ErrInvalid)
	}

	target := cfg.Addr()
	hostKey, hostKeyAlgos, err := HostKeyCallback(target)
	if err != nil {
		return nil, err
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	// 先用带 ctx 的拨号拿 TCP 连接，再做 SSH 握手——ssh.Dial 自己不接受
	// context，握手阶段卡住的话没有别的办法中断。
	var d net.Dialer
	raw, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return nil, fmt.Errorf("连接 %s 失败: %w", target, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(deadline)
	} else {
		_ = raw.SetDeadline(time.Now().Add(timeout))
	}

	conn, chans, reqs, err := ssh.NewClientConn(raw, target, &ssh.ClientConfig{
		User: user,
		Auth: auths,
		// 主机已在 known_hosts 时把算法偏好收窄到我们存过的类型，
		// 服务器才不会递一把我们没见过的 key（见 hostkey.go 的坑注）。
		// 未知主机时为空，走默认偏好。
		HostKeyAlgorithms: hostKeyAlgos,
		HostKeyCallback:   hostKey,
		Timeout:           timeout,
	})
	if err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("ssh 握手失败: %w", err)
	}
	_ = raw.SetDeadline(time.Time{})
	return ssh.NewClient(conn, chans, reqs), nil
}

// authMethods 按配置的验证方式组装认证手段。
func authMethods(cfg Config) ([]ssh.AuthMethod, error) {
	var auths []ssh.AuthMethod
	mode := cfg.Auth
	if mode == "" {
		mode = AuthPassword
	}

	if mode == AuthKey || mode == AuthBoth {
		if path := strings.TrimSpace(cfg.KeyPath); path != "" {
			raw, err := os.ReadFile(ExpandHome(path))
			if err != nil {
				return nil, fmt.Errorf("%w: 读私钥失败: %v", ErrInvalid, err)
			}
			var signer ssh.Signer
			if cfg.Passphrase != "" {
				signer, err = ssh.ParsePrivateKeyWithPassphrase(raw, []byte(cfg.Passphrase))
			} else {
				signer, err = ssh.ParsePrivateKey(raw)
			}
			if err != nil {
				return nil, fmt.Errorf("%w: 解析私钥失败（带密码的私钥要填通行短语）: %v", ErrInvalid, err)
			}
			auths = append(auths, ssh.PublicKeys(signer))
		} else if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
			conn, err := net.Dial("unix", sock)
			if err != nil {
				return nil, fmt.Errorf("%w: 未填私钥路径且连不上 ssh-agent: %v", ErrInvalid, err)
			}
			auths = append(auths, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
		} else {
			return nil, fmt.Errorf("%w: 公钥验证需要填私钥路径（或让 ssh-agent 跑起来）", ErrInvalid)
		}
	}

	if mode == AuthPassword || mode == AuthBoth {
		if cfg.Password == "" {
			return nil, fmt.Errorf("%w: 密码验证需要填 ssh 密码", ErrInvalid)
		}
		auths = append(auths, ssh.Password(cfg.Password))
	}
	return auths, nil
}

// ValidAuth 报告验证方式是不是认得的三档之一（空串不算，调用方自己兜底）。
func ValidAuth(mode string) bool {
	return mode == AuthPassword || mode == AuthKey || mode == AuthBoth
}

// ExpandHome 展开路径开头的 `~`。
func ExpandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}
