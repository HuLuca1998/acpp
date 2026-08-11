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
	manager := acp.NewManager(cfg.MaxSessions)
	defer manager.CloseAll()

	// 对话内容唯一的持久化：每条会话一个 JSONL 转录文件。
	transcripts, err := transcript.NewStore(cfg.TranscriptDir)
	if err != nil {
		return err
	}
	defer transcripts.CloseAll()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.NewRouter(cfg, gdb, manager, transcripts),
		ReadHeaderTimeout: 10 * time.Second,
		// SSE 是长连接，不能给写超时。
		WriteTimeout: 0,
	}

	// 收到中断信号后停止接收新请求，给在途请求 10 秒收尾。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
