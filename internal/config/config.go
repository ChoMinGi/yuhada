package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/joho/godotenv"
)

// Config — .env 또는 환경변수에서 로드.
type Config struct {
	Env             string // "dev" | "prod"
	Addr            string // ":8080"
	DBPath          string // SQLite 파일 경로
	SessionSecret   []byte // 32B 이상
	KakaoClientID   string
	KakaoSecret     string
	KakaoRedirect   string
	LogLevel        string // "debug" | "info" | "warn" | "error"
	AdminBootEmail  string // 첫 실행 시 관리자 식별자 (이메일)
	AdminBootPW     string // 비밀번호 fallback (옵션)
	AdminBootPIN    string // 6자리 PIN — 메인 로그인 수단
	CookieSecure    bool
}

// Load — .env 시도(실패 무시) 후 환경변수로 채움.
func Load() (Config, error) {
	_ = godotenv.Load() // 없어도 OK

	cfg := Config{
		Env:            env("APP_ENV", "dev"),
		Addr:           env("APP_ADDR", ":8080"),
		DBPath:         env("DB_PATH", "./var/yuhada.db"),
		LogLevel:       env("LOG_LEVEL", "info"),
		KakaoClientID:  env("KAKAO_CLIENT_ID", ""),
		KakaoSecret:    env("KAKAO_CLIENT_SECRET", ""),
		KakaoRedirect:  env("KAKAO_REDIRECT_URL", ""),
		AdminBootEmail: env("ADMIN_BOOTSTRAP_EMAIL", ""),
		AdminBootPW:    env("ADMIN_BOOTSTRAP_PW", ""),
		AdminBootPIN:   env("ADMIN_BOOTSTRAP_PIN", ""),
	}

	cfg.CookieSecure, _ = strconv.ParseBool(env("COOKIE_SECURE", "false"))

	secret := env("SESSION_SECRET", "")
	if secret == "" {
		if cfg.Env == "prod" {
			return cfg, fmt.Errorf("SESSION_SECRET required in prod")
		}
		secret = "dev-secret-change-me-in-production-0000000000"
	}
	cfg.SessionSecret = []byte(secret)

	// DB 경로를 절대경로로
	abs, err := filepath.Abs(cfg.DBPath)
	if err == nil {
		cfg.DBPath = abs
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
