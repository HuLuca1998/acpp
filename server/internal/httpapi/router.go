package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"gorm.io/gorm"

	"acpp/server/internal/acp"
	"acpp/server/internal/config"
	"acpp/server/internal/service"
	"acpp/server/internal/transcript"
)

// Version 会随 health 接口返回，方便前端确认后端版本。
const Version = "0.1.0"

// NewRouter 组装全部路由与中间件。
func NewRouter(cfg config.Config, gdb *gorm.DB, manager *acp.Manager, transcripts *transcript.Store) http.Handler {
	sessionService := service.NewSessionService(gdb)
	chatService := service.NewChatService(gdb, sessionService, manager, transcripts)

	agents := agentHandler{agents: service.NewAgentService(gdb)}
	sessions := sessionHandler{sessions: sessionService, chat: chatService}
	chat := chatHandler{chat: chatService}

	api := http.NewServeMux()

	api.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeData(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"version": Version,
		})
	})

	api.HandleFunc("GET /api/agents", agents.list)
	api.HandleFunc("POST /api/agents", agents.create)
	api.HandleFunc("GET /api/agents/{id}", agents.get)
	api.HandleFunc("PUT /api/agents/{id}", agents.update)
	api.HandleFunc("DELETE /api/agents/{id}", agents.remove)

	api.HandleFunc("GET /api/sessions", sessions.list)
	api.HandleFunc("POST /api/sessions", sessions.create)
	api.HandleFunc("GET /api/sessions/{id}", sessions.get)
	api.HandleFunc("DELETE /api/sessions/{id}", sessions.remove)
	api.HandleFunc("GET /api/sessions/{id}/messages", sessions.listMessages)
	api.HandleFunc("GET /api/sessions/{id}/transcript", sessions.transcript)

	// 对话：open 建连、send 发一轮、events 流式收、cancel 中止。
	api.HandleFunc("POST /api/sessions/{id}/open", chat.open)
	api.HandleFunc("POST /api/sessions/{id}/send", chat.send)
	api.HandleFunc("GET /api/sessions/{id}/events", chat.events)
	api.HandleFunc("POST /api/sessions/{id}/cancel", chat.cancel)
	// 会话级配置：模式（审批档）、模型与通用配置项（协作模式、推理档等）。
	api.HandleFunc("POST /api/sessions/{id}/mode", chat.setMode)
	api.HandleFunc("POST /api/sessions/{id}/model", chat.setModel)
	api.HandleFunc("POST /api/sessions/{id}/config", chat.setConfig)
	// 交互式提问的作答回传。
	api.HandleFunc("POST /api/sessions/{id}/elicitation", chat.elicitation)

	root := http.NewServeMux()
	root.Handle("/api/", api)
	if cfg.WebDir != "" {
		root.Handle("/", spaHandler(cfg.WebDir))
	}

	return withRecover(withLogging(withCORS(cfg.CORSOrigins, root)))
}

// spaHandler 托管前端构建产物，未命中的路径回落到 index.html 交给前端路由。
func spaHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	index := filepath.Join(dir, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, index)
	})
}

func pathID(r *http.Request, name string) (uint, error) {
	return parseID(r.PathValue(name))
}

func parseID(raw string) (uint, error) {
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("%w: bad id %q", service.ErrInvalid, raw)
	}
	return uint(id), nil
}
