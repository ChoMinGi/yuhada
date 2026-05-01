package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mingicho/yuhada/internal/db/dbgen"
	"github.com/mingicho/yuhada/internal/util"
)

// WalletService — 충전/차감. 가장 중요한 비즈니스 로직.
//
// 트랜잭션 보장:
//   - db.SetMaxOpenConns(1)로 물리적 직렬화 (DB 레이어)
//   - db.BeginTx 로 논리적 원자성
//   - CHECK (balance >= 0) 로 음수 잔액 물리적 차단 (DDL 레이어)
//   - 서비스에서 사전 검증 → 친절한 에러 메시지
//
// 기존 Postgres RPC (charge_member / deduct_member) 를 Go 레이어로 이식한 것.
type WalletService struct {
	db *sql.DB
}

type WalletResult struct {
	Member        dbgen.Member
	Transaction   dbgen.Transaction
	NewBalance    int64
}

// Charge — 회원 잔액 증가 + 거래 로그.
func (w *WalletService) Charge(ctx context.Context, memberID string, amount int64, memo, createdBy string) (WalletResult, error) {
	return w.mutate(ctx, memberID, amount, "charge", memo, createdBy)
}

// Deduct — 회원 잔액 차감. 잔액 부족 시 ErrInsufficient.
func (w *WalletService) Deduct(ctx context.Context, memberID string, amount int64, memo, createdBy string) (WalletResult, error) {
	return w.mutate(ctx, memberID, -amount, "deduct", memo, createdBy)
}

// mutate — charge/deduct 공통 로직. delta 부호로 구분 (+충전, -차감).
func (w *WalletService) mutate(
	ctx context.Context,
	memberID string,
	delta int64, // 양수 = 충전, 음수 = 차감
	txType, memo, createdBy string,
) (WalletResult, error) {
	if delta == 0 {
		return WalletResult{}, fmt.Errorf("%w: amount must be non-zero", ErrInvalidInput)
	}
	if txType != "charge" && txType != "deduct" && txType != "refund" {
		return WalletResult{}, fmt.Errorf("%w: invalid tx type", ErrInvalidInput)
	}

	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return WalletResult{}, fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	q := dbgen.New(tx)

	// 1. 회원 조회 + 활성 확인
	m, err := q.GetMember(ctx, memberID)
	if errors.Is(err, sql.ErrNoRows) {
		return WalletResult{}, ErrNotFound
	}
	if err != nil {
		return WalletResult{}, fmt.Errorf("get member: %w", err)
	}
	if !m.IsActive {
		return WalletResult{}, ErrInactive
	}

	// 2. 잔액 검증 (차감 시)
	newBalance := m.Balance + delta
	if newBalance < 0 {
		return WalletResult{}, fmt.Errorf("%w: current=%d requested=%d",
			ErrInsufficient, m.Balance, -delta)
	}

	// 3. UPDATE balance
	if err := q.AddToBalance(ctx, dbgen.AddToBalanceParams{
		Balance: delta,
		ID:      memberID,
	}); err != nil {
		return WalletResult{}, fmt.Errorf("add to balance: %w", err)
	}

	// 4. INSERT transaction (append-only)
	absAmount := delta
	if absAmount < 0 {
		absAmount = -absAmount
	}
	t, err := q.InsertTransaction(ctx, dbgen.InsertTransactionParams{
		ID:           util.NewID(),
		MemberID:     memberID,
		Type:         txType,
		Amount:       absAmount,
		BalanceAfter: newBalance,
		Memo:         nullStr(memo),
		CreatedBy:    nullStr(createdBy),
	})
	if err != nil {
		return WalletResult{}, fmt.Errorf("insert tx: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return WalletResult{}, fmt.Errorf("commit: %w", err)
	}
	committed = true

	m.Balance = newBalance // 반영된 값으로 반환
	return WalletResult{
		Member:      m,
		Transaction: t,
		NewBalance:  newBalance,
	}, nil
}
