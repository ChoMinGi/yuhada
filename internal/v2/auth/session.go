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

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
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
// database 파라미터는 인터페이스 호환을 위해 유지하나 memstore 는 사용하지 않는다.
// v1 sqlite3 store 와 같은 DB 파일을 공유할 때 sessions 테이블이 충돌하지 않도록
// 인메모리 스토어를 선택했다. 시연 환경(재시작 시 재로그인 무방)에 적합.
func NewSessionManager(database *sql.DB, secure bool) *scs.SessionManager {
	_ = database
	sm := scs.New()
	sm.Store = memstore.New()
	sm.Lifetime = 12 * time.Hour
	sm.IdleTimeout = 2 * time.Hour
	sm.Cookie.Name = "yuhada_v2_session"
	sm.Cookie.HttpOnly = true
	sm.Cookie.Secure = secure
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Path = "/"
	return sm
}
