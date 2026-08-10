package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"gorm.io/gorm"

	"acpp/server/internal/config"
	"acpp/server/internal/service"
)

// Version 会随 health 接口返回，方便前端确认后端版本。
const Version = "0.1.0"

// NewRouter 组装全部路由与中间件。
func NewRouter(cfg config.Config, gdb *gorm.DB) http.Handler {
	agents := agentHandler{agents: service.NewAgentService(gdb)}
	sessions := sessionHandler{sessions: service.NewSessionService(gdb)}

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
	api.HandleFunc("POST /api/sessions/{id}/messages", sessions.createMessage)

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
