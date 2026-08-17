package datasource

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

const (
	// dialTimeout 是拨号（含 SSH 握手）的上限。连不上就该快点说，
	// 面板上等三十秒不如三秒给个明确失败。
	dialTimeout = 8 * time.Second
	// queryTimeout 是单次调用（可能含多条语句）的总上限。
	queryTimeout = 60 * time.Second
)

// handle 是一条一次性连接。所有出口都必须 defer Close——连接、隧道
// 都挂在它身上，漏一个就是一条泄漏的 SSH 会话。
type handle struct {
	db     *sql.DB
	tunnel *tunnel
}

func (h *handle) Close() {
	if h == nil {
		return
	}
	if h.db != nil {
		_ = h.db.Close()
	}
	h.tunnel.Close()
}

// connect 按配置拨一条 MySQL 连接：开了 SSH 就先连跳板机，MySQL 地址
// 从跳板机视角解析。database 覆盖数据源里配的默认库（空则用默认）。
func connect(ctx context.Context, src *model.DataSource, database string) (*handle, error) {
	if strings.TrimSpace(src.Host) == "" {
		return nil, fmt.Errorf("%w: 主机不能为空", service.ErrInvalid)
	}
	port := src.Port
	if port <= 0 {
		port = 3306
	}

	cfg, err := baseConfig(src.Params)
	if err != nil {
		return nil, err
	}
	cfg.User = src.User
	cfg.Passwd = src.Password
	cfg.DBName = strings.TrimSpace(database)
	if cfg.DBName == "" {
		cfg.DBName = strings.TrimSpace(src.Database)
	}
	cfg.Timeout = dialTimeout

	h := &handle{}
	if src.SSHEnabled {
		t, err := dialTunnel(ctx, src)
		if err != nil {
			return nil, err
		}
		h.tunnel = t
		cfg.Net = tunnelNet
		cfg.Addr = t.addr(src.Host, port)
	} else {
		cfg.Net = "tcp"
		cfg.Addr = net.JoinHostPort(src.Host, strconv.Itoa(port))
	}

	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		h.Close()
		return nil, fmt.Errorf("%w: 连接参数有误: %v", service.ErrInvalid, err)
	}
	db := sql.OpenDB(connector)
	// 一次性连接：池子留 1 条，多条语句必须落在同一个物理连接上，
	// 否则 `USE db` / 临时表 / 会话变量在下一条语句里就不见了。
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	h.db = db

	if err := db.PingContext(ctx); err != nil {
		h.Close()
		return nil, fmt.Errorf("连接失败: %w", err)
	}
	// 刻意不下发任何会话设置（MAX_EXECUTION_TIME 之类）：连上去的库是
	// 用户的生产环境，我们只读它、不改它的行为。超时与行数护栏全部在
	// 本进程这侧执行（见 exec.go）。
	return h, nil
}

// baseConfig 把用户填的额外参数解析成驱动配置的基底。
//
// 走 ParseDSN 而不是手工塞 Params：tls / charset / loc 这些在驱动里是
// **具名字段**不是普通参数，手工塞会被静默忽略；先解析一个只带参数的
// DSN，再覆盖连接信息，两边都不会漏。
func baseConfig(params string) (*mysql.Config, error) {
	dsn := "/"
	if p := strings.TrimSpace(strings.TrimPrefix(params, "?")); p != "" {
		dsn += "?" + p
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: 额外参数解析失败: %v", service.ErrInvalid, err)
	}
	// 时间一律按字符串取回：ParseTime 会把 DATETIME 转成带时区的 time.Time，
	// 再序列化成 JSON 就与库里看到的字面值对不上了。
	cfg.ParseTime = false
	cfg.AllowNativePasswords = true
	// 多结果集不开：语句由我们自己切分后逐条执行（见 split.go），
	// 这样每条都有独立的耗时、影响行数与错误定位。
	cfg.MultiStatements = false
	return cfg, nil
}
