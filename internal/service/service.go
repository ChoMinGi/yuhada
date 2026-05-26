// Package service — 비즈니스 로직 계층.
// DB 접근은 sqlc generated dbgen.Queries 경유. 핸들러는 service만 호출.
package service

import (
	"database/sql"
	"errors"

	"github.com/mingicho/yuhada/internal/crypto"
	"github.com/mingicho/yuhada/internal/db/dbgen"
)

// Services — 의존성 컨테이너. main에서 1회 생성 후 핸들러에 주입.
type Services struct {
	DB      *sql.DB
	Queries *dbgen.Queries
	Enc     *crypto.Enc // nil = 암호화 비활성
	Member  *MemberService
	Wallet  *WalletService
	Tx      *TransactionService
	Stats   *StatsService
	Admin   *AdminService
}

// New — 모든 서비스 초기화.
func New(database *sql.DB, enc *crypto.Enc) *Services {
	q := dbgen.New(database)
	s := &Services{
		DB:      database,
		Queries: q,
		Enc:     enc,
	}
	s.Member = &MemberService{db: database, q: q, enc: enc}
	s.Wallet = &WalletService{db: database, enc: enc}
	s.Tx = &TransactionService{db: database, q: q, enc: enc}
	s.Stats = &StatsService{db: database, q: q}
	s.Admin = &AdminService{db: database, q: q}
	return s
}

// ─────────────────────────────────────────────
// 공통 에러
// ─────────────────────────────────────────────
var (
	ErrNotFound        = errors.New("not found")
	ErrInsufficient    = errors.New("insufficient balance")
	ErrInactive        = errors.New("member is inactive (lost card)")
	ErrInvalidInput    = errors.New("invalid input")
	ErrDuplicatePhone  = errors.New("phone already exists")
	ErrDuplicateCard   = errors.New("card uuid already exists")
	ErrPasswordInvalid = errors.New("password invalid")
)
