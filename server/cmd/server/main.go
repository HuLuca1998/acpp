package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"acpp/server/internal/acp"
	"acpp/server/internal/config"
	"acpp/server/internal/datasource"
	"acpp/server/internal/db"
	"acpp/server/internal/httpapi"
	"acpp/server/internal/model"
	"acpp/server/internal/orch"
	"acpp/server/internal/project"
	"acpp/server/internal/service"
	"acpp/server/internal/system"
	"acpp/server/internal/transcript"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()

	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	// 打开数据库前先准备数据目录（自动创建 ~/.acpp，存量数据一次性迁入）。
	if err := config.PrepareData(cfg); err != nil {
		return err
	}

	gdb, err := db.Open(cfg)
	if err != nil {
		return err
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	// 服务退出时必须回收全部 agent 子进程，否则会留下一堆孤儿。
	// 每条会话注入控制端技能包（<dataDir>/skillpack），屏蔽机器级 skill。
	manager := acp.NewManager(cfg.MaxSessions, cfg.TurnTimeout, filepath.Join(cfg.DataDir, "skillpack"))
	defer manager.CloseAll()

	// 对话内容唯一的持久化：每条会话一个 JSONL 转录文件。
	transcripts, err := transcript.NewStore(cfg.TranscriptDir)
	if err != nil {
		return err
	}
	defer transcripts.CloseAll()

	// 刚启动时没有任何 agent 进程活着，遗留的「进行中」都是上次进程留下的
	// 谎言——归一成可续聊的空闲态（error/ended 保留，它们的信息有意义）。
	if err := gdb.Model(&model.Session{}).
		Where("state = ?", model.SessionActive).
		Update("state", model.SessionIdle).Error; err != nil {
		slog.Warn("normalize stale sessions", "err", err)
	}

	// 业务服务全部在装配层构建，HTTP 层只收成品（分层：httpapi 不碰 db）。
	agentService := service.NewAgentService(gdb)
	sessionService := service.NewSessionService(gdb)
	skillUsage := service.NewSkillUsageService(gdb, cfg.DataDir)
	chatService := service.NewChatService(gdb, sessionService, manager, transcripts, skillUsage)

	// 内置工具（claude/codex）缺失时补建：清库/全新安装后开箱即有，
	// 设置页的两个分区始终有对象可配。新建的后台探测一次能力缓存。
	if created, err := agentService.EnsureDefaults(context.Background()); err != nil {
		slog.Warn("ensure default agents", "err", err)
	} else {
		for _, id := range created {
			go func(id uint) {
				probeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				if _, err := chatService.ProbeAgent(probeCtx, id); err != nil {
					slog.Warn("probe default agent", "id", id, "err", err)
				}
			}(id)
		}
	}
	// 编排（adr-006）：角色 + 主会话 + spawn 子会话。启动时预置内置角色
	// 模板（一次性，删除不复活）。
	roleService := orch.NewRoleService(gdb)
	if err := roleService.EnsureDefaults(context.Background()); err != nil {
		slog.Warn("ensure default roles", "err", err)
	}
	orchService := orch.NewService(gdb, roleService, manager, transcripts,
		skillUsage, cfg.DataDir, filepath.Join(cfg.DataDir, "skillpack"), cfg.Addr)
	// 编排会话的遗留 active 同样归一。
	if err := gdb.Model(&model.OrchSession{}).
		Where("state = ?", model.SessionActive).
		Update("state", model.SessionIdle).Error; err != nil {
		slog.Warn("normalize stale orch sessions", "err", err)
	}
	if err := gdb.Model(&model.OrchTask{}).
		Where("state = ?", model.OrchTaskRunning).
		Updates(map[string]any{"state": model.OrchTaskFailed, "result": "服务重启，任务中断"}).Error; err != nil {
		slog.Warn("normalize stale orch tasks", "err", err)
	}

	// 多租户（adr-007）：局域网访客的身份与目录隔离。租户 root 落在默认
	// 工作区下（~/acpp/<租户>），owner 由 loopback 判定，不入表。
	tenantService := service.NewTenantService(gdb, service.DefaultCwd())

	// 项目面：扫工作区根下的 git 仓库，克隆落到 <root>/<组织>/<仓库>。
	projectService := project.NewService(gdb)

	// 数据库数据源：配置面 + 挂给会话的 MCP 工具面。两个方向都经这里
	// 装配——chat 只认得 MCPMounter 接口，datasource 只认得 Sessions 接口，
	// 两个业务包因此不互相 import。
	datasourceService := datasource.NewService(gdb, sessionService, cfg.Addr)
	chatService.SetDataSources(datasourceService)
	orchService.SetDataSources(datasourceService)

	terminalService := service.NewTerminalService(cfg.MaxTerminals)
	// 工作区终端的 pty 随服务退出统一回收，不留孤儿 shell。
	defer terminalService.Shutdown()

	// 版本检查：启动即查一次，此后每天刷新；结果缓存在内存供设置页读取。
	updateService := system.NewUpdater(cfg.UpdateRepo)
	updateService.StartPeriodicCheck(context.Background(), 24*time.Hour)

	handler := httpapi.NewRouter(cfg, httpapi.Services{
		Agents:      agentService,
		Sessions:    sessionService,
		Chat:        chatService,
		Terminals:   terminalService,
		System:      system.NewService(gdb, cfg),
		Skills:      service.NewSkillService(cfg.DataDir, skillUsage),
		SkillUsage:  skillUsage,
		Update:      updateService,
		Roles:       roleService,
		Orch:        orchService,
		Tenants:     tenantService,
		Projects:    projectService,
		DataSources: datasourceService,
	})
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// SSE 是长连接，不能给写超时。
		WriteTimeout: 0,
	}

	// 收到中断信号后停止接收新请求，给在途请求 10 秒收尾。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 空闲子进程定期回收：上下文在 agent 侧持久化，续聊时无感恢复。
	chatService.StartIdleReaper(ctx, cfg.IdleTimeout)
	// 老会话的消息数缓存回填，后台低优跑一次。
	go chatService.BackfillMessageCounts(ctx)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Addr, "dsn", cfg.DSN)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
