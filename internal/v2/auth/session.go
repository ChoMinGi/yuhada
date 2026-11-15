// Package auth — v2 전용 세션·관리자 인증.
//
// v1과 쿠키/세션 테이블을 분리하고 같은 로그인 유지 정책을 사용한다.
// 운영 PIN 검증은 기존 관리자 서비스에서 수행한다.
package auth

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
)

// 세션 키.
const (
	SessionKeyRole    = "v2_role"
	SessionKeyAdminID = "v2_admin_id"
	RoleAdmin         = "admin"
)

// NewSessionManager — 재배포 후에도 유지되는 전용 세션. v1과 동일한 30일/12시간 정책.
func NewSessionManager(database *sql.DB, secure bool) *scs.SessionManager {
	sm := scs.New()
	if database == nil {
		// Isolated authentication tests do not need a database.
		sm.Store = memstore.New()
	} else {
		sm.Store = sqlite3store.NewWithConfig(database, sqlite3store.Config{
			TableName:       "admin_sessions_v2",
			CleanUpInterval: 5 * time.Minute,
		})
	}
	sm.Lifetime = 30 * 24 * time.Hour
	sm.IdleTimeout = 12 * time.Hour
	sm.Cookie.Name = "yuhada_v2_session"
	sm.Cookie.HttpOnly = true
	sm.Cookie.Secure = secure
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Path = "/"
	return sm
}
