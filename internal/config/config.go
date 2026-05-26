package config

import (
	"encoding/hex"
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
	EncryptionKey   []byte // 32B AES-256 키 (빈값이면 암호화 비활성)
	AligoKey        string // 알리고 SMS API 키
	AligoUserID     string // 알리고 계정 ID
	AligoSender     string // 발신번호
	KakaoClientID   string
	KakaoSecret     string
	KakaoRedirect   string
	LogLevel        string // "debug" | "info" | "warn" | "error"
	AdminBootEmail  string // 첫 실행 시 관리자 식별자 (이메일)
	AdminBootPW     string // 비밀번호 fallback (옵션)
	AdminBootPIN    string // 6자리 PIN — 메인 로그인 수단
	CookieSecure    bool
}

// IsProd — 프로덕션 환경 여부.
func (c Config) IsProd() bool { return c.Env == "prod" }

// Load — .env 시도(실패 무시) 후 환경변수로 채움.
func Load() (Config, error) {
	_ = godotenv.Load() // 없어도 OK

	cfg := Config{
		Env:            normalizeEnv(env("APP_ENV", "dev")),
		Addr:           env("APP_ADDR", ":8080"),
		DBPath:         env("DB_PATH", "./var/yuhada.db"),
		LogLevel:       env("LOG_LEVEL", "info"),
		AligoKey:       env("ALIGO_API_KEY", ""),
		AligoUserID:    env("ALIGO_USER_ID", ""),
		AligoSender:    env("ALIGO_SENDER", ""),
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
		if cfg.IsProd() {
			return cfg, fmt.Errorf("SESSION_SECRET required in prod")
		}
		secret = "dev-secret-change-me-in-production-0000000000"
	}
	cfg.SessionSecret = []byte(secret)

	// 암호화 키 (hex-encoded, 64자 = 32바이트)
	if keyHex := env("ENCRYPTION_KEY", ""); keyHex != "" {
		k, err := hex.DecodeString(keyHex)
		if err != nil {
			return cfg, fmt.Errorf("ENCRYPTION_KEY: invalid hex: %w", err)
		}
		if len(k) != 32 {
			return cfg, fmt.Errorf("ENCRYPTION_KEY: need 32 bytes (64 hex chars), got %d", len(k))
		}
		cfg.EncryptionKey = k
	}

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

// normalizeEnv — "production" → "prod" 통일.
func normalizeEnv(s string) string {
	if s == "production" {
		return "prod"
	}
	return s
}
