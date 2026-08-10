package config

import (
	"os"
	"strings"
)

// Config 是服务的全部可调项，全部来自环境变量，带可用的默认值。
type Config struct {
	// Addr 是 HTTP 监听地址。
	Addr string
	// DSN 是 sqlite 数据库文件路径。
	DSN string
	// CORSOrigins 为空时不下发 CORS 头（生产同源部署即为此情形）。
	CORSOrigins []string
	// WebDir 指向前端构建产物；为空时不托管静态文件。
	WebDir string
	// Debug 打开 gorm 的 SQL 日志。
	Debug bool
}

// Load 从环境变量读取配置。
func Load() Config {
	return Config{
		Addr:        env("ACP_ADDR", "127.0.0.1:8080"),
		DSN:         env("ACP_DSN", "data/acp.db"),
		CORSOrigins: splitAndTrim(env("ACP_CORS_ORIGINS", "http://localhost:5173")),
		WebDir:      env("ACP_WEB_DIR", ""),
		Debug:       env("ACP_DEBUG", "") != "",
	}
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func splitAndTrim(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
