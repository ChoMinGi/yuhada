// Package service — v2 비즈니스 로직 계층.
// DB 접근은 v2 sqlc generated dbgen.Queries 경유. 핸들러는 service만 호출.
//
// 기존 members/transactions 원장을 공유하고 일반 충전·차감은 v1 업무 로직을 재사용한다.
// 상품 판매 기록만 별도 운영 테이블에 추가하며 시험용 v2_* 테이블은 사용하지 않는다.
package service

import (
	"database/sql"
	"errors"

	"github.com/mingicho/yuhada/internal/crypto"
	legacy "github.com/mingicho/yuhada/internal/service"
	"github.com/mingicho/yuhada/internal/v2/db/dbgen"
)

// Services — v2 의존성 컨테이너. main 에서 1회 생성 후 핸들러에 주입.
type Services struct {
	DB      *sql.DB
	Queries *dbgen.Queries
	Enc     *crypto.Enc

	Member     *MemberService
	Plan       *PlanService
	Charge     *ChargeService
	Membership *MembershipService
	Preset     *PresetService
	Sales      *SalesService
}

// New — 모든 v2 서비스 초기화.
func New(database *sql.DB, enc *crypto.Enc) *Services {
	q := dbgen.New(database)
	s := &Services{
		DB:      database,
		Queries: q,
		Enc:     enc,
	}
	s.Member = &MemberService{db: database, q: q, enc: enc}
	s.Plan = &PlanService{db: database, q: q}
	s.Charge = &ChargeService{db: database, q: q, enc: enc}
	s.Charge.wallet = legacy.New(database, enc).Wallet
	s.Membership = &MembershipService{db: database, q: q}
	s.Preset = &PresetService{db: database, q: q}
	s.Sales = &SalesService{db: database, q: q, enc: enc}
	return s
}

// ─────────────────────────────────────────────
// 공통 에러 (v1 service 에러와 별도로 v2 자체 정의)
// ─────────────────────────────────────────────
var (
	ErrNotFound       = errors.New("not found")
	ErrInsufficient   = errors.New("insufficient balance")
	ErrInactive       = errors.New("member is inactive")
	ErrInvalidInput   = errors.New("invalid input")
	ErrDuplicatePhone = errors.New("phone already exists")
	ErrDuplicateCard  = errors.New("card uuid already exists")
	ErrPlanInactive   = errors.New("plan is inactive")
	ErrMembershipDone = errors.New("membership already terminated")
)
