package service

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/mingicho/yuhada/internal/db/dbgen"
	"github.com/mingicho/yuhada/internal/util"
)

type AdminService struct {
	db *sql.DB
	q  *dbgen.Queries
}

var pinRE = regexp.MustCompile(`^\d{6}$`)

// IsValidPIN — 정확히 숫자 6자리.
func IsValidPIN(s string) bool { return pinRE.MatchString(s) }

// Bootstrap — 앱 부팅 시 admin 없으면 생성, 있으면 PIN 갱신 (idempotent).
//
//   email: 식별자 (DB unique key)
//   pin:   6자리 PIN. 빈 문자열이면 PIN 설정 안 함 (비번만 사용)
//   pw:    옵션 비번 fallback. 빈 문자열이면 미설정
func (s *AdminService) Bootstrap(ctx context.Context, email, pin, pw string) error {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return nil
	}

	// 기존 admin 조회
	existing, err := s.q.GetAdminByEmail(ctx, email)
	switch {
	case err == nil:
		// 이미 있으면 PIN만 업데이트 (옵션)
		if pin != "" && IsValidPIN(pin) {
			hash, err := bcrypt.GenerateFromPassword([]byte(pin), 12)
			if err != nil {
				return fmt.Errorf("hash pin: %w", err)
			}
			if err := s.q.SetAdminPin(ctx, dbgen.SetAdminPinParams{
				PinHash: sql.NullString{String: string(hash), Valid: true},
				ID:      existing.ID,
			}); err != nil {
				return fmt.Errorf("set pin: %w", err)
			}
			slog.Info("admin PIN updated", "email", email)
		}
		return nil

	case errors.Is(err, sql.ErrNoRows):
		// 신규 생성

	default:
		return fmt.Errorf("query admin: %w", err)
	}

	if pin == "" && pw == "" {
		return fmt.Errorf("bootstrap admin needs PIN or password")
	}
	if pin != "" && !IsValidPIN(pin) {
		return fmt.Errorf("bootstrap PIN must be 6 digits")
	}

	// pw_hash는 NOT NULL이라 항상 채워야 함. PIN만 주어졌으면 임의의 큰 값으로 채움 (검증 시 사용 안 함).
	pwToHash := pw
	if pwToHash == "" {
		pwToHash = "disabled-" + util.NewID()
	}
	pwHash, err := bcrypt.GenerateFromPassword([]byte(pwToHash), 12)
	if err != nil {
		return fmt.Errorf("hash pw: %w", err)
	}

	created, err := s.q.CreateAdminUser(ctx, dbgen.CreateAdminUserParams{
		ID:     util.NewID(),
		Email:  email,
		PwHash: string(pwHash),
		Name:   sql.NullString{String: "사장님", Valid: true},
		Role:   "owner",
	})
	if err != nil {
		return fmt.Errorf("create admin: %w", err)
	}

	if pin != "" {
		pinHash, err := bcrypt.GenerateFromPassword([]byte(pin), 12)
		if err != nil {
			return fmt.Errorf("hash pin: %w", err)
		}
		if err := s.q.SetAdminPin(ctx, dbgen.SetAdminPinParams{
			PinHash: sql.NullString{String: string(pinHash), Valid: true},
			ID:      created.ID,
		}); err != nil {
			return fmt.Errorf("set pin: %w", err)
		}
	}
	slog.Info("admin bootstrapped", "email", email, "with_pin", pin != "")
	return nil
}

// VerifyPIN — 모든 활성 admin에 대해 PIN 비교 (1~5명 가정, bcrypt cost 12 OK).
// 매칭 성공 시 해당 admin user 반환.
//
// Timing safety: 항상 모든 활성 admin에 대해 비교 후 결과 반환 (early-return 방지).
func (s *AdminService) VerifyPIN(ctx context.Context, pin string) (dbgen.AdminUser, error) {
	if !IsValidPIN(pin) {
		return dbgen.AdminUser{}, ErrPasswordInvalid
	}

	admins, err := s.q.ListActiveAdmins(ctx)
	if err != nil {
		return dbgen.AdminUser{}, err
	}

	var match dbgen.AdminUser
	matched := false
	for _, a := range admins {
		if !a.PinHash.Valid {
			continue
		}
		if err := bcrypt.CompareHashAndPassword([]byte(a.PinHash.String), []byte(pin)); err == nil {
			if !matched {
				match = a
				matched = true
			}
			// 계속 루프 (timing-equal)
		}
	}
	if !matched {
		return dbgen.AdminUser{}, ErrPasswordInvalid
	}

	if err := s.q.UpdateAdminLastLogin(ctx, match.ID); err != nil {
		slog.Warn("update last_login failed", "err", err)
	}
	return match, nil
}

// Verify — 이메일+비번 검증 (fallback 또는 정상 운영 시 비활성화 가능).
func (s *AdminService) Verify(ctx context.Context, email, password string) (dbgen.AdminUser, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	u, err := s.q.GetAdminByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		return dbgen.AdminUser{}, ErrPasswordInvalid
	}
	if err != nil {
		return dbgen.AdminUser{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PwHash), []byte(password)); err != nil {
		return dbgen.AdminUser{}, ErrPasswordInvalid
	}
	if err := s.q.UpdateAdminLastLogin(ctx, u.ID); err != nil {
		slog.Warn("update last_login failed", "err", err)
	}
	return u, nil
}

// Get — 세션 admin id 재조회.
func (s *AdminService) Get(ctx context.Context, id string) (dbgen.AdminUser, error) {
	u, err := s.q.GetAdmin(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return dbgen.AdminUser{}, ErrNotFound
	}
	return u, err
}

// 사용 안 함 — subtle 패키지 link 보존 (timing-safe 비교 도구로 추후 활용).
var _ = subtle.ConstantTimeCompare
