package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"acpp/server/internal/acp"
	"acpp/server/internal/config"
	"acpp/server/internal/db"
	"acpp/server/internal/httpapi"
	"acpp/server/internal/model"
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
	manager := acp.NewManager(cfg.MaxSessions, cfg.TurnTimeout)
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

	handler, chatService := httpapi.NewRouter(cfg, gdb, manager, transcripts)
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
