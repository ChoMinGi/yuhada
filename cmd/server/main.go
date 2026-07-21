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

	"github.com/go-chi/chi/v5"

	"github.com/mingicho/yuhada/internal/auth"
	"github.com/mingicho/yuhada/internal/config"
	"github.com/mingicho/yuhada/internal/crypto"
	"github.com/mingicho/yuhada/internal/db"
	"github.com/mingicho/yuhada/internal/handler"
	"github.com/mingicho/yuhada/internal/service"
	"github.com/mingicho/yuhada/internal/sms"

	// V2 — dev only (옵션 C 정책, isolation-rules.md §6)
	v2auth "github.com/mingicho/yuhada/internal/v2/auth"
	v2handler "github.com/mingicho/yuhada/internal/v2/handler"
	v2kakao "github.com/mingicho/yuhada/internal/v2/kakao"
	v2migrate "github.com/mingicho/yuhada/internal/v2/migrate"
	v2seed "github.com/mingicho/yuhada/internal/v2/seed"
	v2service "github.com/mingicho/yuhada/internal/v2/service"
	v2sms "github.com/mingicho/yuhada/internal/v2/sms"
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

	// 암호화 핸들
	enc, err := crypto.New(cfg.EncryptionKey)
	if err != nil {
		logger.Error("encryption init failed", "err", err)
		os.Exit(1)
	}
	if enc != nil {
		logger.Info("PII encryption enabled")
	}

	// 서비스 레이어
	services := service.New(database, enc)

	// 관리자 부트스트랩 — PIN 우선, 비번은 fallback.
	bootCtx, bootCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := services.Admin.Bootstrap(bootCtx, cfg.AdminBootEmail, cfg.AdminBootPIN, cfg.AdminBootPW); err != nil {
		logger.Warn("admin bootstrap failed", "err", err)
	}
	bootCancel()

	// 기존 평문 레코드 암호화 마이그레이션 (1회성)
	migrCtx, migrCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := services.Member.MigrateEncryption(migrCtx); err != nil {
		logger.Warn("encryption migration failed", "err", err)
	}
	migrCancel()

	// SMS 클라이언트
	smsClient := sms.New(cfg.AligoKey, cfg.AligoUserID, cfg.AligoSender)
	if smsClient.Enabled() {
		logger.Info("SMS notifications enabled", "sender", cfg.AligoSender)
	}

	deps := &handler.Deps{
		Session:     session,
		Services:    services,
		SMS:         smsClient,
		EnableDebug: !cfg.IsProd(),
	}
	router := handler.NewRouter(deps)

	// ───── V2 마운트 ─────
	// dev: 무조건 마운트. prod: env V2_ENABLE=true 로 opt-in (시연용).
	// v2 는 v2_ prefix 로 격리되어 v1 DB 와 같은 파일에 공존 가능 (isolation-rules §1).
	root := chi.NewMux()
	root.Mount("/", router)

	if !cfg.IsProd() || cfg.V2Enable {
		if err := os.MkdirAll(filepath.Dir(cfg.DBV2Path), 0o750); err != nil {
			logger.Error("v2 mkdir failed", "err", err)
			os.Exit(1)
		}
		v2DB, err := db.Open(cfg.DBV2Path)
		if err != nil {
			logger.Error("v2 db open failed", "err", err, "path", cfg.DBV2Path)
			os.Exit(1)
		}
		defer v2DB.Close()
		logger.Info("v2 db opened", "path", cfg.DBV2Path, "prod_opt_in", cfg.IsProd() && cfg.V2Enable)

		// R7: 자동 마이그레이션 (goose in-process). seed 이전.
		if err := v2migrate.Up(v2DB); err != nil {
			logger.Error("v2 migrate failed", "err", err)
			os.Exit(1)
		}
		logger.Info("v2 migrate applied")

		v2Session := v2auth.NewSessionManager(v2DB, cfg.CookieSecure)
		v2Services := v2service.New(v2DB, enc)
		v2SMS := v2sms.New()

		// 시드는 실패해도 부팅 계속 (Warn 만).
		seedCtx, seedCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := v2seed.EnsureSeeded(seedCtx, v2DB, v2Services); err != nil {
			logger.Warn("v2 seed failed", "err", err)
		}
		seedCancel()

		v2Kakao := &v2kakao.Config{
			ClientID:     cfg.KakaoClientID,
			ClientSecret: cfg.KakaoSecret,
			RedirectURI:  cfg.KakaoRedirectV2,
		}
		v2Router := v2handler.NewRouter(&v2handler.Deps{
			Session:      v2Session,
			Services:     v2Services,
			SMS:          v2SMS,
			AdminPIN:     cfg.AdminPinV2,
			Kakao:        v2Kakao,
			AdminKakaoID: cfg.AdminKakaoIDV2,
			EnableDebug:  true,
		})
		root.Mount("/v2", v2Router)
		mode := "dev"
		if cfg.IsProd() {
			mode = "prod (V2_ENABLE=true opt-in — 시연 모드)"
		}
		logger.Info("v2 mounted at /v2", "mode", mode)
	} else {
		logger.Info("v2 mount skipped (prod default — V2_ENABLE 미설정)")
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           root,
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
