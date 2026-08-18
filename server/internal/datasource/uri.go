package datasource

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"acpp/server/internal/model"
)

// 连接 URI 导出：一条链接换一整套连接参数，贴进 Navicat 或别的客户端直接能用。
//
// **导出带真实密码**（用户拍板）。这是刻意的取舍：不带密码的链接对方还得
// 再问一次密码，等于没省事。代价是这条链接本身就是凭证——界面上标红提示，
// 端点也只对 owner 开放（整个 /api/datasources 前缀都是）。
//
// 生成放后端而不是前端：编辑已有连接时密码不下发到浏览器（响应里只有
// hasPassword 标志位），前端根本拼不出带密码的链接。解析仍在前端
//（web/src/lib/db-uri.ts）——那是用户自己粘进来的文本，不涉及已存的秘密。

// URIExport 是一条连接的两种导出写法。
type URIExport struct {
	// Navicat 是 Navicat「URI」按钮认的原样格式。
	Navicat string `json:"navicat"`
	// Standard 是通用写法（DBeaver、TablePlus、命令行都认）。
	Standard string `json:"standard"`
}

// ExportURI 生成一条数据源的两种 URI 写法。
func ExportURI(src *model.DataSource) URIExport {
	return URIExport{
		Navicat:  navicatURI(src),
		Standard: standardURI(src),
	}
}

// navicatURI 拼 `navicat://conn.mysql?Conn.*` 格式。
//
// 键名与顺序照 Navicat 自己导出的样子（字母序），贴回去时更像原生。
// 值不做 URL 编码——实测 Navicat 导出的链接里 `@`、`/` 都是裸的，编码过
// 反而它自己认不回来。
func navicatURI(src *model.DataSource) string {
	port := src.Port
	if port <= 0 {
		port = 3306
	}
	pairs := [][2]string{
		{"Conn.Host", src.Host},
	}
	if db := strings.TrimSpace(src.Database); db != "" {
		pairs = append(pairs, [2]string{"Conn.Database", db})
	}
	pairs = append(pairs,
		[2]string{"Conn.Name", connectionName(src)},
		[2]string{"Conn.Password", src.Password},
		[2]string{"Conn.Port", strconv.Itoa(port)},
	)

	if src.SSHEnabled {
		sshPort := src.SSHPort
		if sshPort <= 0 {
			sshPort = 22
		}
		pairs = append(pairs,
			[2]string{"Conn.SSH.AuthenticationMethod", navicatAuthName(src.SSHAuth)},
			[2]string{"Conn.SSH.Host", src.SSHHost},
			[2]string{"Conn.SSH.Port", strconv.Itoa(sshPort)},
			[2]string{"Conn.SSH.Username", src.SSHUser},
		)
		if src.SSHAuth != SSHAuthKey && src.SSHPassword != "" {
			pairs = append(pairs, [2]string{"Conn.SSH.Password", src.SSHPassword})
		}
		if src.SSHAuth != SSHAuthPassword {
			if src.SSHKeyPath != "" {
				pairs = append(pairs, [2]string{"Conn.SSH.PrivateKey", src.SSHKeyPath})
			}
			if src.SSHPassphrase != "" {
				pairs = append(pairs, [2]string{"Conn.SSH.Passphrase", src.SSHPassphrase})
			}
		}
	}

	pairs = append(pairs,
		[2]string{"Conn.UseHTTP", "false"},
		[2]string{"Conn.UseSSH", strconv.FormatBool(src.SSHEnabled)},
		[2]string{"Conn.UseSSL", "false"},
		[2]string{"Conn.UseSocketFile", "false"},
		[2]string{"Conn.Username", src.User},
	)

	parts := make([]string, 0, len(pairs))
	for _, kv := range pairs {
		parts = append(parts, kv[0]+"="+kv[1])
	}
	return "navicat://conn.mysql?" + strings.Join(parts, "&")
}

// standardURI 拼 `mysql://user:pass@host:port/db?params` 格式。
// 这条走正规 URL 编码——通用客户端按 RFC 解析，密码里的 `@` 不编码会断在那里。
func standardURI(src *model.DataSource) string {
	port := src.Port
	if port <= 0 {
		port = 3306
	}
	u := url.URL{
		Scheme: "mysql",
		Host:   fmt.Sprintf("%s:%d", src.Host, port),
	}
	if src.User != "" {
		if src.Password != "" {
			u.User = url.UserPassword(src.User, src.Password)
		} else {
			u.User = url.User(src.User)
		}
	}
	if db := strings.TrimSpace(src.Database); db != "" {
		u.Path = "/" + db
	}

	q := url.Values{}
	if params := strings.TrimSpace(src.Params); params != "" {
		if parsed, err := url.ParseQuery(params); err == nil {
			q = parsed
		}
	}
	if src.SSHEnabled && src.SSHHost != "" {
		sshPort := src.SSHPort
		if sshPort <= 0 {
			sshPort = 22
		}
		q.Set("sshHost", src.SSHHost)
		q.Set("sshPort", strconv.Itoa(sshPort))
		if src.SSHUser != "" {
			q.Set("sshUser", src.SSHUser)
		}
		q.Set("sshAuth", firstNonEmpty(src.SSHAuth, SSHAuthPassword))
		if src.SSHAuth != SSHAuthPassword && src.SSHKeyPath != "" {
			q.Set("sshKeyPath", src.SSHKeyPath)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// connectionName 是 Navicat 那边的连接名，用 `<项目>-<环境>` 拼
// （前端解析时按同样的规则拆回来）。
func connectionName(src *model.DataSource) string {
	switch {
	case src.Project != "" && src.Env != "":
		return src.Project + "-" + src.Env
	case src.Project != "":
		return src.Project
	default:
		return src.Env
	}
}

// navicatAuthName 把我们的三档映射回 Navicat 的验证方法。
// Navicat 没有「密码和公钥」这一档，both 导出成 PublicKey——会丢一点信息，
// 但那条 URI 贴回 Navicat 时至少是能用的。
func navicatAuthName(auth string) string {
	if auth == SSHAuthKey || auth == SSHAuthBoth {
		return "PublicKey"
	}
	return "Password"
}
