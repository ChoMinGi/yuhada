package auth

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"
)

// NewSessionManager — scs 매니저를 SQLite 스토어로 구성.
//
// 만료 정책:
//   - Lifetime    30일 — 절대 상한. 이후 무조건 재로그인.
//   - IdleTimeout 12시간 — 마지막 활동 이후 미사용 시 만료.
//
// 매장 영업 시나리오: 영업 종료 후 밤새 미사용 → 다음날 아침 PIN 재입력.
// 영업 중에는 페이지 이동/액션마다 갱신되어 끊김 없음.
func NewSessionManager(database *sql.DB, secure bool) *scs.SessionManager {
	sm := scs.New()
	sm.Store = sqlite3store.New(database)
	sm.Lifetime = 30 * 24 * time.Hour
	sm.IdleTimeout = 12 * time.Hour
	sm.Cookie.Name = "yh_session"
	sm.Cookie.HttpOnly = true
	sm.Cookie.Secure = secure
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Path = "/"
	return sm
}
