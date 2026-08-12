package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config 是服务的全部可调项，来自环境变量与固定配置文件，带可用默认值。
type Config struct {
	// Addr 是 HTTP 监听地址。
	Addr string
	// DataDir 是数据根目录：优先级 ACP_DATA_DIR > ~/.acpp/config.json
	// 里用户选定的目录 > 默认 ~/.acpp。db 与转录默认都派生于它。
	DataDir string
	// DSN 是 sqlite 数据库文件路径。
	DSN string
	// TranscriptDir 存放每条会话的 ACP 线级转录（JSONL）。
	TranscriptDir string
	// CORSOrigins 为空时不下发 CORS 头（生产同源部署即为此情形）。
	CORSOrigins []string
	// WebDir 指向前端构建产物；为空时不托管静态文件。
	WebDir string
	// Debug 打开 gorm 的 SQL 日志。
	Debug bool
	// MaxSessions 是同时活着的 agent 子进程上限，防止进程泄漏。
	MaxSessions int
	// IdleTimeout 是空闲会话子进程的回收时限；上下文已持久化在 agent 侧
	//（acpSessionId），回收后下次发消息用 session/load 无感恢复。0 = 不回收。
	IdleTimeout time.Duration
	// TurnTimeout 是单轮硬上限，默认 0 = 不限时——长程任务跑几个小时是
	// 正常使用方式；turn 进行中不受空闲回收影响。要保护就显式设。
	TurnTimeout time.Duration
	// MaxTerminals 是每个会话的工作区终端实例上限，防止 pty 进程失控。
	MaxTerminals int
}

// Load 从环境变量与固定配置文件读取配置。
func Load() Config {
	dataDir := env("ACP_DATA_DIR", "")
	if dataDir == "" {
		dataDir = SavedDataDir()
	}
	if dataDir == "" {
		dataDir = ConfigHome()
	}
	dsn := env("ACP_DSN", filepath.Join(dataDir, "acp.db"))
	return Config{
		Addr:          env("ACP_ADDR", "127.0.0.1:48080"),
		DataDir:       dataDir,
		DSN:           dsn,
		TranscriptDir: env("ACP_TRANSCRIPT_DIR", filepath.Join(filepath.Dir(dsn), "transcripts")),
		CORSOrigins:   splitAndTrim(env("ACP_CORS_ORIGINS", "http://localhost:45173")),
		WebDir:        env("ACP_WEB_DIR", ""),
		Debug:         env("ACP_DEBUG", "") != "",
		MaxSessions:   envInt("ACP_MAX_SESSIONS", 8),
		IdleTimeout:   envDuration("ACP_IDLE_TIMEOUT", 10*time.Minute),
		TurnTimeout:   envDuration("ACP_TURN_TIMEOUT", 0),
		MaxTerminals:  envInt("ACP_MAX_TERMINALS", 5),
	}
}

func envDuration(key string, fallback time.Duration) time.Duration {
	raw := env(key, "")
	if raw == "" {
		return fallback
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return v
}

func envInt(key string, fallback int) int {
	v, err := strconv.Atoi(env(key, ""))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
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
