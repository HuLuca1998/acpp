package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"acpp/server/internal/config"
	"acpp/server/internal/orch"
	"acpp/server/internal/project"
	"acpp/server/internal/service"
	"acpp/server/internal/system"
)

// Services 是路由需要的全部业务服务，由装配层（cmd/server）构建后传入——
// HTTP 层只做路由与编解码，不负责连库与组装依赖。
type Services struct {
	Agents     *service.AgentService
	Sessions   *service.SessionService
	Chat       *service.ChatService
	Terminals  *service.TerminalService
	System     *system.Service
	Skills     *service.SkillService
	SkillUsage *service.SkillUsageService
	Update     *system.Updater
	Roles      *orch.RoleService
	Orch       *orch.Service
	Tenants    *service.TenantService
	Projects   *project.Service
}

// NewRouter 组装全部路由与中间件。
func NewRouter(cfg config.Config, svcs Services) http.Handler {
	agents := agentHandler{agents: svcs.Agents, chat: svcs.Chat}
	sessions := sessionHandler{sessions: svcs.Sessions, chat: svcs.Chat}
	chat := chatHandler{chat: svcs.Chat, sessions: svcs.Sessions}
	system := systemHandler{system: svcs.System, update: svcs.Update}
	skills := skillHandler{skills: svcs.Skills, usage: svcs.SkillUsage}

	api := http.NewServeMux()

	// 版本号在 config.Version（构建期 ldflags 注入），随 health 返回供前端确认。
	api.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeData(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"version": config.Version,
		})
	})

	// 身份：邀请兑换、身份自查、退出（adr-007）。这三条是公开路径，
	// 未认证也能访问——否则前端连「我需要邀请链接」都问不出来。
	auth := authHandler{tenants: svcs.Tenants}
	api.HandleFunc("GET /api/auth/me", auth.me)
	api.HandleFunc("POST /api/auth/redeem", auth.redeem)
	api.HandleFunc("POST /api/auth/logout", auth.logout)

	// 租户管理：owner 专属（isOwnerOnly 按前缀覆盖）。
	tenants := tenantHandler{tenants: svcs.Tenants, addr: cfg.Addr}
	api.HandleFunc("GET /api/tenants", tenants.list)
	api.HandleFunc("POST /api/tenants", tenants.create)
	api.HandleFunc("PUT /api/tenants/{id}", tenants.update)
	api.HandleFunc("POST /api/tenants/{id}/rotate", tenants.rotate)
	api.HandleFunc("DELETE /api/tenants/{id}", tenants.remove)

	// 目录浏览：供工作目录/文件选择器导航本机目录（浏览器拿不到绝对路径）。
	// ?files=1 时连同文件一起列（@ 文件引用用）。
	api.HandleFunc("GET /api/fs/dirs", func(w http.ResponseWriter, r *http.Request) {
		listing, err := service.ListDirs(scopeOf(r), r.URL.Query().Get("path"), r.URL.Query().Get("files") == "1")
		if err != nil {
			writeError(w, err)
			return
		}
		writeData(w, http.StatusOK, listing)
	})

	// 就地新建子目录：给新会话开工作目录时不用离开选择器。
	api.HandleFunc("POST /api/fs/dirs", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Path string `json:"path"`
			Name string `json:"name"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, err)
			return
		}
		entry, err := service.CreateDir(scopeOf(r), req.Path, req.Name)
		if err != nil {
			writeError(w, err)
			return
		}
		writeData(w, http.StatusCreated, entry)
	})

	// 项目：工作区根下的仓库目录（磁盘即事实源）。克隆是后台任务，
	// 进度轮询 clones；repos 是克隆对话框的远端仓库清单（gh CLI）。
	projects := projectHandler{projects: svcs.Projects}
	api.HandleFunc("GET /api/projects", projects.list)
	api.HandleFunc("POST /api/projects", projects.create)
	api.HandleFunc("GET /api/projects/repos", projects.repos)
	api.HandleFunc("GET /api/projects/clones", projects.clones)
	api.HandleFunc("POST /api/projects/clone", projects.clone)
	api.HandleFunc("DELETE /api/projects/{name...}", projects.remove)

	// 系统配置：数据目录查看 / 迁移（拷贝式，重启后生效）。
	api.HandleFunc("GET /api/system", system.get)
	api.HandleFunc("PUT /api/system/data-dir", system.migrate)
	// 环境体检与一键安装：依赖清单是后端白名单，安装命令不接受用户输入。
	api.HandleFunc("GET /api/system/env", system.env)
	api.HandleFunc("POST /api/system/env/install", system.envInstall)
	// 版本检查（GitHub Releases，缓存 + ?force=1 现查）与一键更新重启。
	api.HandleFunc("GET /api/system/update", system.updateInfo)
	api.HandleFunc("POST /api/system/update/apply", system.updateApply)

	// 技能库：磁盘为事实源（~/.acpp/skills + skillpack 分发链接），无数据库表。
	api.HandleFunc("GET /api/skills", skills.list)
	api.HandleFunc("GET /api/skills/usage", skills.usageTop)
	api.HandleFunc("POST /api/skills", skills.create)
	api.HandleFunc("GET /api/skills/{name}", skills.get)
	api.HandleFunc("PUT /api/skills/{name}", skills.update)
	api.HandleFunc("DELETE /api/skills/{name}", skills.remove)
	// 技能附属文件（references/ scripts/ 等）：文本文件可读写，二进制只列出。
	// 脚本头部元信息（desc/usage/arg/opt/env 注释键值）驱动前端控件与试运行。
	api.HandleFunc("GET /api/skills/{name}/scripts", skills.listScripts)
	api.HandleFunc("POST /api/skills/{name}/scripts/run", skills.runScript)
	api.HandleFunc("GET /api/skills/{name}/files", skills.listFiles)
	api.HandleFunc("GET /api/skills/{name}/files/{path...}", skills.getFile)
	api.HandleFunc("PUT /api/skills/{name}/files/{path...}", skills.putFile)
	api.HandleFunc("DELETE /api/skills/{name}/files/{path...}", skills.removeFile)

	api.HandleFunc("GET /api/agents", agents.list)
	api.HandleFunc("POST /api/agents", agents.create)
	api.HandleFunc("GET /api/agents/{id}", agents.get)
	api.HandleFunc("PUT /api/agents/{id}", agents.update)
	api.HandleFunc("DELETE /api/agents/{id}", agents.remove)
	// 重探 agent 的统一设置能力（flavor 与模型清单），同步返回最新记录。
	api.HandleFunc("POST /api/agents/{id}/probe", agents.probe)
	// 配置页勾选：更新 models/commands 的启用状态。
	api.HandleFunc("PUT /api/agents/{id}/catalog", agents.catalog)

	// 工作区数据面按 cwd 工作，普通会话与编排会话只差记录来源。
	// 解析 cwd 的同时做归属校验：工作区的全部数据面（文件树、预览、git、
	// 终端）都经这一步，隔离因此只有一个执行点（adr-007）。
	sessionCwd := func(r *http.Request, id uint) (string, error) {
		session, err := svcs.Sessions.Get(r.Context(), scopeOf(r), id)
		if err != nil {
			return "", err
		}
		return session.Cwd, nil
	}
	// 编排整体是 owner 专属（isOwnerOnly 已按前缀拦截），到这里不必再分身份。
	orchCwd := func(r *http.Request, id uint) (string, error) {
		orchSession, err := svcs.Orch.Get(r.Context(), id)
		if err != nil {
			return "", err
		}
		return orchSession.Cwd, nil
	}
	workspace := workspaceHandler{cwdOf: sessionCwd}

	api.HandleFunc("GET /api/sessions", sessions.list)
	api.HandleFunc("POST /api/sessions", sessions.create)
	api.HandleFunc("GET /api/sessions/{id}", sessions.get)
	api.HandleFunc("DELETE /api/sessions/{id}", sessions.remove)
	api.HandleFunc("GET /api/sessions/{id}/messages", sessions.listMessages)
	api.HandleFunc("GET /api/sessions/{id}/transcript", sessions.transcript)

	// 工作区面板数据面：文件树（depth≤2 一次返回，gitignore 过滤）与文件预览。
	api.HandleFunc("GET /api/sessions/{id}/fs/entries", workspace.tree)
	api.HandleFunc("GET /api/sessions/{id}/fs/file", workspace.file)
	// git 数据面：overview 供 diff/commit 两面板共享，diff/commit 按需取全文。
	api.HandleFunc("GET /api/sessions/{id}/git/overview", workspace.gitOverview)
	api.HandleFunc("GET /api/sessions/{id}/git/diff", workspace.gitDiff)
	api.HandleFunc("GET /api/sessions/{id}/git/commits/{sha}", workspace.gitCommit)
	// 分支面：会话底部的分支控件读 branches，切换走 checkout；
	// worktree 是「勾一下就有个隔离工作区」的落地（adr-007）。
	api.HandleFunc("GET /api/sessions/{id}/git/branches", workspace.branches)
	api.HandleFunc("GET /api/sessions/{id}/git/history", workspace.history)
	api.HandleFunc("GET /api/sessions/{id}/git/compare", workspace.compare)
	api.HandleFunc("POST /api/sessions/{id}/git/checkout", workspace.checkout)
	api.HandleFunc("POST /api/sessions/{id}/git/worktrees", workspace.createWorktree)
	api.HandleFunc("DELETE /api/sessions/{id}/git/worktrees", workspace.removeWorktree)

	// 工作区终端：REST 管生命周期，ws 桥 pty 双向流（adr-002 M3）。
	terminals := terminalHandler{
		cwdOf:          sessionCwd,
		terms:          svcs.Terminals,
		originPatterns: corsHosts(cfg.CORSOrigins),
	}
	api.HandleFunc("POST /api/sessions/{id}/terminals", terminals.create)
	api.HandleFunc("GET /api/sessions/{id}/terminals", terminals.list)
	api.HandleFunc("DELETE /api/sessions/{id}/terminals/{tid}", terminals.remove)
	api.HandleFunc("GET /api/sessions/{id}/terminals/{tid}/ws", terminals.attach)

	// 角色：编排里可雇佣的子代理定义（adr-006）。
	roles := roleHandler{roles: svcs.Roles}
	api.HandleFunc("GET /api/roles", roles.list)
	api.HandleFunc("POST /api/roles", roles.create)
	api.HandleFunc("GET /api/roles/{id}", roles.get)
	api.HandleFunc("PUT /api/roles/{id}", roles.update)
	api.HandleFunc("DELETE /api/roles/{id}", roles.remove)

	// 编排：主会话 + spawn 的任务子会话 + agent 回连的 MCP 端点（adr-006）。
	orch := orchHandler{orch: svcs.Orch}
	api.HandleFunc("GET /api/orchestrator/sessions", orch.list)
	api.HandleFunc("POST /api/orchestrator/sessions", orch.create)
	api.HandleFunc("GET /api/orchestrator/sessions/{id}", orch.get)
	api.HandleFunc("DELETE /api/orchestrator/sessions/{id}", orch.remove)
	api.HandleFunc("POST /api/orchestrator/sessions/{id}/send", orch.send)
	api.HandleFunc("GET /api/orchestrator/sessions/{id}/events", orch.events)
	api.HandleFunc("GET /api/orchestrator/sessions/{id}/messages", orch.messages)
	api.HandleFunc("POST /api/orchestrator/sessions/{id}/cancel", orch.cancel)
	api.HandleFunc("POST /api/orchestrator/sessions/{id}/stop", orch.stop)
	api.HandleFunc("GET /api/orchestrator/sessions/{id}/settings", orch.getSettings)
	api.HandleFunc("PUT /api/orchestrator/sessions/{id}/settings", orch.settings)
	api.HandleFunc("POST /api/orchestrator/sessions/{id}/permission", orch.permission)
	api.HandleFunc("POST /api/orchestrator/sessions/{id}/elicitation", orch.elicitation)
	api.HandleFunc("GET /api/orchestrator/sessions/{id}/tasks", orch.tasks)
	api.HandleFunc("GET /api/orchestrator/tasks/{tid}/events", orch.taskEvents)
	api.HandleFunc("GET /api/orchestrator/tasks/{tid}/messages", orch.taskMessages)
	api.HandleFunc("POST /api/orchestrator/tasks/{tid}/cancel", orch.taskCancel)
	api.HandleFunc("POST /api/orchestrator/tasks/{tid}/permission", orch.taskPermission)
	api.HandleFunc("POST /api/orchestrator/tasks/{tid}/elicitation", orch.taskElicitation)
	api.HandleFunc("/api/mcp/{token}", orch.mcp)

	// 编排主会话的完整工作区（升级不降级）：文件树/预览、git 数据面、
	// 终端与转录，全部复用普通会话的数据面实现，只有 cwd 来源不同。
	orchWorkspace := workspaceHandler{cwdOf: orchCwd}
	api.HandleFunc("GET /api/orchestrator/sessions/{id}/fs/entries", orchWorkspace.tree)
	api.HandleFunc("GET /api/orchestrator/sessions/{id}/fs/file", orchWorkspace.file)
	api.HandleFunc("GET /api/orchestrator/sessions/{id}/git/overview", orchWorkspace.gitOverview)
	api.HandleFunc("GET /api/orchestrator/sessions/{id}/git/diff", orchWorkspace.gitDiff)
	api.HandleFunc("GET /api/orchestrator/sessions/{id}/git/commits/{sha}", orchWorkspace.gitCommit)
	api.HandleFunc("GET /api/orchestrator/sessions/{id}/git/branches", orchWorkspace.branches)
	api.HandleFunc("GET /api/orchestrator/sessions/{id}/git/history", orchWorkspace.history)
	api.HandleFunc("GET /api/orchestrator/sessions/{id}/git/compare", orchWorkspace.compare)
	api.HandleFunc("POST /api/orchestrator/sessions/{id}/git/checkout", orchWorkspace.checkout)
	api.HandleFunc("POST /api/orchestrator/sessions/{id}/git/worktrees", orchWorkspace.createWorktree)
	api.HandleFunc("DELETE /api/orchestrator/sessions/{id}/git/worktrees", orchWorkspace.removeWorktree)
	orchTerminals := terminalHandler{
		cwdOf:          orchCwd,
		terms:          svcs.Terminals,
		originPatterns: corsHosts(cfg.CORSOrigins),
		keyOffset:      orchTerminalKeyOffset,
	}
	api.HandleFunc("POST /api/orchestrator/sessions/{id}/terminals", orchTerminals.create)
	api.HandleFunc("GET /api/orchestrator/sessions/{id}/terminals", orchTerminals.list)
	api.HandleFunc("DELETE /api/orchestrator/sessions/{id}/terminals/{tid}", orchTerminals.remove)
	api.HandleFunc("GET /api/orchestrator/sessions/{id}/terminals/{tid}/ws", orchTerminals.attach)
	api.HandleFunc("GET /api/orchestrator/sessions/{id}/transcript", orch.transcript)

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
	// 身份中间件只包 API：前端页面本身必须对未认证访客可加载，否则
	// 邀请链接 `/?invite=xxx` 会先撞 401 白屏，连兑换都发不出去。
	root.Handle("/api/", withIdentity(svcs.Tenants, api))
	if cfg.WebDir != "" {
		root.Handle("/", spaHandler(cfg.WebDir))
	}

	return withRecover(withLogging(withCORS(cfg.CORSOrigins, root)))
}

// corsHosts 把 CORS origin 列表转成 ws 升级用的 host pattern
// （websocket.AcceptOptions 只认 host，不带 scheme）。
func corsHosts(origins []string) []string {
	hosts := make([]string, 0, len(origins))
	for _, o := range origins {
		o = strings.TrimPrefix(strings.TrimPrefix(o, "https://"), "http://")
		if o != "" {
			hosts = append(hosts, o)
		}
	}
	return hosts
}

// spaHandler 托管前端构建产物，未命中的路径回落到 index.html 交给前端路由。
func spaHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	index := filepath.Join(dir, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 缓存策略是版本更新生效的前提：入口 html 必须每次协商（no-cache，
		// 304 才复用），否则 WKWebView/浏览器按启发式缓存端出旧界面，
		// 「更新完还要手动刷新」；vite 产物带内容哈希，可放心永久缓存。
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
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
