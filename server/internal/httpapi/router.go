package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"acpp/server/internal/config"
	"acpp/server/internal/datasource"
	"acpp/server/internal/mcpcall"
	"acpp/server/internal/project"
	"acpp/server/internal/service"
	"acpp/server/internal/system"
	"acpp/server/internal/titler"
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
	// Titler 生成会话标题；设置页读写它的配置。
	Titler      *titler.Service
	Tenants     *service.TenantService
	Projects    *project.Service
	DataSources *datasource.Service
	// MCPCalls 是 MCP 工具调用的观测记录，工具台读它。
	MCPCalls *mcpcall.Service
}

// NewRouter 组装全部路由与中间件。
func NewRouter(cfg config.Config, svcs Services) http.Handler {
	agents := agentHandler{agents: svcs.Agents, chat: svcs.Chat}
	sessions := sessionHandler{sessions: svcs.Sessions, chat: svcs.Chat}
	chat := chatHandler{chat: svcs.Chat, sessions: svcs.Sessions}
	system := systemHandler{system: svcs.System, update: svcs.Update, titler: svcs.Titler, busyTurns: svcs.Chat.ActiveTurnCount}
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
		listing, err := service.ListDirs(scopeOf(r), r.URL.Query().Get("path"), r.URL.Query().Get("files") == "1", r.URL.Query().Get("hidden") == "1")
		if err != nil {
			writeError(w, err)
			return
		}
		writeData(w, http.StatusOK, listing)
	})

	// 选择器侧边栏的默认位置（家目录一族 + 工作区根；租户只有自己的 root）。
	api.HandleFunc("GET /api/fs/places", func(w http.ResponseWriter, r *http.Request) {
		writeData(w, http.StatusOK, service.Places(scopeOf(r)))
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
	// 工作区根：agent 干活的地方与租户 root 的父目录，立刻生效。
	api.HandleFunc("PUT /api/system/workspace-dir", system.workspaceDir)
	// 环境体检与一键安装：依赖清单是后端白名单，安装命令不接受用户输入。
	api.HandleFunc("GET /api/system/env", system.env)
	api.HandleFunc("POST /api/system/env/install", system.envInstall)
	// 版本检查（GitHub Releases，缓存 + ?force=1 现查）与一键更新重启。
	// 会话标题模型（本机 ollama）：配置读写 + 模型清单 + 当场试生成。
	api.HandleFunc("GET /api/system/title-model", system.titleModel)
	api.HandleFunc("PUT /api/system/title-model", system.saveTitleModel)
	api.HandleFunc("GET /api/system/title-model/models", system.titleModels)
	api.HandleFunc("POST /api/system/title-model/test", system.titleModelTest)

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

	// 工作区数据面按 cwd 工作。解析 cwd 的同时做归属校验：工作区的全部
	// 数据面（文件树、预览、git、终端）都经这一步，隔离因此只有一个
	// 执行点（adr-007）。
	sessionCwd := func(r *http.Request) (string, error) {
		id, err := pathID(r, "id")
		if err != nil {
			return "", err
		}
		session, err := svcs.Sessions.Get(r.Context(), scopeOf(r), id)
		if err != nil {
			return "", err
		}
		return session.Cwd, nil
	}
	// 草稿态：会话还没建，目录由请求直接给。看文件、看 git 状态本来就
	// 只需要一个目录——不该等到「发出第一条消息」之后才允许（adr-002）。
	// 路径闸照旧：租户给什么都逃不出自己的 root。
	draftCwd := func(r *http.Request) (string, error) {
		return scopeOf(r).GuardPath(r.URL.Query().Get("cwd"))
	}
	// 本地文件上传：落在各自身份的家目录下，@ 引用直接用它的路径。
	uploads := uploadHandler{}
	api.HandleFunc("GET /api/uploads", uploads.list)
	api.HandleFunc("POST /api/uploads", uploads.create)
	api.HandleFunc("DELETE /api/uploads", uploads.remove)

	workspace := workspaceHandler{cwdOf: sessionCwd}

	// 草稿态工作区：会话还没建，目录由 `?cwd=` 给。看文件与 git 状态只需要
	// 一个目录——选完工作目录就该能用，不必先发一条消息把会话建出来。
	draft := workspaceHandler{cwdOf: draftCwd}
	api.HandleFunc("GET /api/workspace/fs/entries", draft.tree)
	api.HandleFunc("GET /api/workspace/fs/file", draft.file)
	api.HandleFunc("GET /api/workspace/fs/download", draft.download)
	api.HandleFunc("GET /api/workspace/git/overview", draft.gitOverview)
	api.HandleFunc("GET /api/workspace/git/diff", draft.gitDiff)
	api.HandleFunc("GET /api/workspace/git/commits/{sha}", draft.gitCommit)
	api.HandleFunc("GET /api/workspace/git/branches", draft.branches)
	api.HandleFunc("GET /api/workspace/git/history", draft.history)
	api.HandleFunc("GET /api/workspace/git/compare", draft.compare)
	api.HandleFunc("POST /api/workspace/git/checkout", draft.checkout)
	api.HandleFunc("POST /api/workspace/git/commit", draft.commit)
	api.HandleFunc("POST /api/workspace/git/push", draft.push)
	api.HandleFunc("POST /api/workspace/git/pull", draft.pull)
	api.HandleFunc("POST /api/workspace/git/merge", draft.merge)
	api.HandleFunc("POST /api/workspace/git/branches", draft.createBranch)
	api.HandleFunc("DELETE /api/workspace/git/branches/{name}", draft.deleteBranch)
	api.HandleFunc("POST /api/workspace/git/discard", draft.discard)
	api.HandleFunc("POST /api/workspace/git/worktrees", draft.createWorktree)
	api.HandleFunc("DELETE /api/workspace/git/worktrees", draft.removeWorktree)

	api.HandleFunc("GET /api/sessions", sessions.list)
	// 概览统计：按天趋势 + agent/状态分布，聚合在有全量数据的这一侧做。
	api.HandleFunc("GET /api/sessions/overview", sessions.overview)
	api.HandleFunc("POST /api/sessions", sessions.create)
	api.HandleFunc("GET /api/sessions/{id}", sessions.get)
	api.HandleFunc("DELETE /api/sessions/{id}", sessions.remove)
	api.HandleFunc("GET /api/sessions/{id}/messages", sessions.listMessages)
	api.HandleFunc("GET /api/sessions/{id}/transcript", sessions.transcript)

	// 工作区面板数据面：文件树（depth≤2 一次返回，gitignore 过滤）与文件预览。
	api.HandleFunc("GET /api/sessions/{id}/fs/entries", workspace.tree)
	api.HandleFunc("GET /api/sessions/{id}/fs/file", workspace.file)
	// 原样下载（右键「下载」）：与预览分开——预览会截断、二进制只标记。
	api.HandleFunc("GET /api/sessions/{id}/fs/download", workspace.download)
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
	// git 写操作：提交/推送/拉取/合并/分支增删/丢弃改动。每条都把 git 的
	// 原话带回界面——失败原因本身就是下一步该做什么的说明。
	api.HandleFunc("POST /api/sessions/{id}/git/commit", workspace.commit)
	api.HandleFunc("POST /api/sessions/{id}/git/push", workspace.push)
	api.HandleFunc("POST /api/sessions/{id}/git/pull", workspace.pull)
	api.HandleFunc("POST /api/sessions/{id}/git/merge", workspace.merge)
	api.HandleFunc("POST /api/sessions/{id}/git/branches", workspace.createBranch)
	api.HandleFunc("DELETE /api/sessions/{id}/git/branches/{name}", workspace.deleteBranch)
	api.HandleFunc("POST /api/sessions/{id}/git/discard", workspace.discard)
	api.HandleFunc("POST /api/sessions/{id}/git/worktrees", workspace.createWorktree)
	api.HandleFunc("DELETE /api/sessions/{id}/git/worktrees", workspace.removeWorktree)

	// 数据库数据源：管理面是 owner 专属（isOwnerOnly 按前缀覆盖），
	// 会话面按 cwd 所属项目过滤——界面能看到的范围与 AI 能操作的范围
	// 是同一个（datasource.Service.ForCwd 是唯一执行点）。
	datasources := datasourceHandler{sources: svcs.DataSources, cwdOf: sessionCwd}
	api.HandleFunc("GET /api/datasources", datasources.list)
	api.HandleFunc("POST /api/datasources", datasources.create)
	api.HandleFunc("GET /api/datasources/{id}", datasources.get)
	api.HandleFunc("PUT /api/datasources/{id}", datasources.update)
	api.HandleFunc("DELETE /api/datasources/{id}", datasources.remove)
	api.HandleFunc("POST /api/datasources/{id}/test", datasources.test)
	// 连接 URI 导出（带密码，owner 专属面）。导入在前端解析，不经后端。
	api.HandleFunc("GET /api/datasources/{id}/uri", datasources.uri)
	// 配置页选库用：唯一不受「一条连接一个库」约束的读法（那时还没绑定）。
	api.HandleFunc("POST /api/datasources/probe-databases", datasources.probeDatabases)
	// 配置页 SSH 页签单独测隧道：不碰 MySQL，报错就知道卡在哪层。
	api.HandleFunc("POST /api/datasources/probe-ssh", datasources.probeSSH)
	api.HandleFunc("GET /api/datasources/{id}/databases", datasources.databases)
	api.HandleFunc("GET /api/datasources/{id}/tables", datasources.tables)
	api.HandleFunc("GET /api/datasources/{id}/schema", datasources.schema)
	api.HandleFunc("POST /api/datasources/{id}/query", datasources.query)
	// 会话侧（斜杠命令与 @ 引用的数据源）：只列当前项目的，id 不在项目内
	// 按不存在处理。
	api.HandleFunc("GET /api/sessions/{id}/datasources", datasources.sessionList)
	api.HandleFunc("GET /api/sessions/{id}/datasources/{dsid}/databases", datasources.sessionDatabases)
	api.HandleFunc("GET /api/sessions/{id}/datasources/{dsid}/tables", datasources.sessionTables)
	// 草稿态：会话还没建，项目由 `?cwd=` 给的目录决定——选完工作目录就该
	// 能引用数据库，与草稿工作区（文件树/git）同一先例（adr-002）。
	draftDatasources := datasourceHandler{sources: svcs.DataSources, cwdOf: draftCwd}
	api.HandleFunc("GET /api/workspace/datasources", draftDatasources.sessionList)
	api.HandleFunc("GET /api/workspace/datasources/{dsid}/databases", draftDatasources.sessionDatabases)
	api.HandleFunc("GET /api/workspace/datasources/{dsid}/tables", draftDatasources.sessionTables)
	// agent 回连的数据库工具端点（token 是每条会话专属凭证）。
	api.HandleFunc("/api/mcp/db/{token}", datasources.mcp)

	// 工具台（页面 /tools）：看工具面、人工试运行、发自定义 JSON-RPC、
	// 回看调用记录。owner 专属，与上面那条公开的回连端点刻意分前缀。
	tools := toolsHandler{sources: svcs.DataSources, calls: svcs.MCPCalls}
	api.HandleFunc("GET /api/tools/servers", tools.servers)
	api.HandleFunc("POST /api/tools/inspect", tools.inspect)
	api.HandleFunc("GET /api/tools/calls", tools.callList)
	api.HandleFunc("GET /api/tools/calls/stats", tools.callStats)
	api.HandleFunc("DELETE /api/tools/calls", tools.callClear)

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

	// 对话：open 建连、send 发一轮、events 流式收、cancel 中止。
	api.HandleFunc("POST /api/sessions/{id}/open", chat.open)
	api.HandleFunc("POST /api/sessions/{id}/send", chat.send)
	api.HandleFunc("GET /api/sessions/{id}/events", chat.events)
	api.HandleFunc("POST /api/sessions/{id}/cancel", chat.cancel)
	api.HandleFunc("GET /api/sessions/{id}/subagents/{threadId}/output", chat.subagentOutput)
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
