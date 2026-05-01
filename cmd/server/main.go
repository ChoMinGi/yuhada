// cmd/server — YUHADA HAIR 서버 엔트리포인트.
//
// 실행:
//   APP_ENV=dev go run ./cmd/server
//   또는 make dev
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

	"github.com/mingicho/yuhada/internal/auth"
	"github.com/mingicho/yuhada/internal/config"
	"github.com/mingicho/yuhada/internal/db"
	"github.com/mingicho/yuhada/internal/handler"
	"github.com/mingicho/yuhada/internal/service"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	// DB 디렉토리 존재 확인
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o750); err != nil {
		logger.Error("mkdir db dir failed", "err", err)
		os.Exit(1)
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		logger.Error("db open failed", "err", err, "path", cfg.DBPath)
		os.Exit(1)
	}
	defer database.Close()
	logger.Info("db opened", "path", cfg.DBPath)

	session := auth.NewSessionManager(database, cfg.CookieSecure)

	// 서비스 레이어
	services := service.New(database)

	// 관리자 부트스트랩 — PIN 우선, 비번은 fallback.
	bootCtx, bootCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := services.Admin.Bootstrap(bootCtx, cfg.AdminBootEmail, cfg.AdminBootPIN, cfg.AdminBootPW); err != nil {
		logger.Warn("admin bootstrap failed", "err", err)
	}
	bootCancel()

	deps := &handler.Deps{
		Session:     session,
		Services:    services,
		EnableDebug: cfg.Env != "prod", // dev/test에서만 /debug/* 노출
	}
	router := handler.NewRouter(deps)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// graceful shutdown
	errCh := make(chan error, 1)
	go func() {
		logger.Info("server starting", "addr", cfg.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("shutdown signal received", "signal", sig)
	case err := <-errCh:
		logger.Error("server crashed", "err", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", "err", err)
		os.Exit(1)
	}
	logger.Info("shutdown complete")
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: lvl,
	}))
}
