// Package auth — v2 전용 세션·관리자 인증.
//
// 격리 원칙(isolation-rules.md §1-5):
//   - v1 세션 매니저와 완전히 분리된 인스턴스 (별도 sqlite3store)
//   - 쿠키 이름 다름: yuhada_v2_session
//   - PIN 검증은 v2 admin_users 를 두지 않고 cfg.AdminPinV2 와 단순 비교 (핸들러 책임)
package auth

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"
)

// 세션 키.
const (
	SessionKeyRole = "v2_role"
	RoleAdmin      = "admin"
)

// NewSessionManager — v2 전용 scs 매니저. v1과 쿠키 이름·수명이 다르다.
//
// 만료:
//   - Lifetime    12시간 — v2는 실험 환경. 하루 이상 세션 유지 필요 없음.
//   - IdleTimeout 2시간
func NewSessionManager(database *sql.DB, secure bool) *scs.SessionManager {
	sm := scs.New()
	sm.Store = sqlite3store.New(database)
	sm.Lifetime = 12 * time.Hour
	sm.IdleTimeout = 2 * time.Hour
	sm.Cookie.Name = "yuhada_v2_session"
	sm.Cookie.HttpOnly = true
	sm.Cookie.Secure = secure
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Path = "/"
	return sm
}
