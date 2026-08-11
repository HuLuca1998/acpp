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

// NewRouter 组装全部路由与中间件。除 handler 外把 ChatService 一并交回，
// 供装配层挂后台职责（空闲回收）。
func NewRouter(cfg config.Config, gdb *gorm.DB, manager *acp.Manager, transcripts *transcript.Store) (http.Handler, *service.ChatService) {
	sessionService := service.NewSessionService(gdb)
	chatService := service.NewChatService(gdb, sessionService, manager, transcripts)

	agents := agentHandler{agents: service.NewAgentService(gdb), chat: chatService}
	sessions := sessionHandler{sessions: sessionService, chat: chatService}
	chat := chatHandler{chat: chatService}

	api := http.NewServeMux()

	api.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeData(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"version": Version,
		})
	})

	// 目录浏览：供工作目录/文件选择器导航本机目录（浏览器拿不到绝对路径）。
	// ?files=1 时连同文件一起列（@ 文件引用用）。
	api.HandleFunc("GET /api/fs/dirs", func(w http.ResponseWriter, r *http.Request) {
		listing, err := service.ListDirs(r.URL.Query().Get("path"), r.URL.Query().Get("files") == "1")
		if err != nil {
			writeError(w, err)
			return
		}
		writeData(w, http.StatusOK, listing)
	})

	api.HandleFunc("GET /api/agents", agents.list)
	api.HandleFunc("POST /api/agents", agents.create)
	api.HandleFunc("GET /api/agents/{id}", agents.get)
	api.HandleFunc("PUT /api/agents/{id}", agents.update)
	api.HandleFunc("DELETE /api/agents/{id}", agents.remove)
	// 重探 agent 的统一设置能力（flavor 与模型清单），同步返回最新记录。
	api.HandleFunc("POST /api/agents/{id}/probe", agents.probe)
	// 配置页勾选：更新 models/commands 的启用状态。
	api.HandleFunc("PUT /api/agents/{id}/catalog", agents.catalog)

	workspace := workspaceHandler{sessions: sessionService}

	api.HandleFunc("GET /api/sessions", sessions.list)
	api.HandleFunc("POST /api/sessions", sessions.create)
	api.HandleFunc("GET /api/sessions/{id}", sessions.get)
	api.HandleFunc("DELETE /api/sessions/{id}", sessions.remove)
	api.HandleFunc("GET /api/sessions/{id}/messages", sessions.listMessages)
	api.HandleFunc("GET /api/sessions/{id}/transcript", sessions.transcript)

	// 工作区面板数据面：文件树（depth≤2 一次返回，gitignore 过滤）与文件预览。
	api.HandleFunc("GET /api/sessions/{id}/fs/entries", workspace.tree)
	api.HandleFunc("GET /api/sessions/{id}/fs/file", workspace.file)

	// 对话：open 建连、send 发一轮、events 流式收、cancel 中止。
	api.HandleFunc("POST /api/sessions/{id}/open", chat.open)
	api.HandleFunc("POST /api/sessions/{id}/send", chat.send)
	api.HandleFunc("GET /api/sessions/{id}/events", chat.events)
	api.HandleFunc("POST /api/sessions/{id}/cancel", chat.cancel)
	// 会话级统一设置：模型/思考深度/权限档/plan/fast，逐项可选。
	api.HandleFunc("PUT /api/sessions/{id}/settings", chat.settings)
	// 交互式提问的作答与权限裁决回传。
	api.HandleFunc("POST /api/sessions/{id}/elicitation", chat.elicitation)
	api.HandleFunc("POST /api/sessions/{id}/permission", chat.permission)

	root := http.NewServeMux()
	root.Handle("/api/", api)
	if cfg.WebDir != "" {
		root.Handle("/", spaHandler(cfg.WebDir))
	}

	return withRecover(withLogging(withCORS(cfg.CORSOrigins, root))), chatService
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
