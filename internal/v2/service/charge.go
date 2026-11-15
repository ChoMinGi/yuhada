package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/mingicho/yuhada/internal/crypto"
	legacy "github.com/mingicho/yuhada/internal/service"
	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/v2/db/dbgen"
)

// ChargeService — plan 기반 정액권 판매 + 일반 충전 + 차감 + 환불/만료.
// pricing-model.md §5 트랜잭션 의미론 준수: BEGIN IMMEDIATE → plan SELECT →
// transactions INSERT → memberships INSERT → members UPDATE balance.
type ChargeService struct {
	db     *sql.DB
	q      *dbgen.Queries
	enc    *crypto.Enc
	wallet *legacy.WalletService
}

// ChargeResult — 충전 결과. MembershipID 는 plan 기반일 때만 non-empty.
type ChargeResult struct {
	Member       dbgen.V2Member
	Transaction  dbgen.V2Transaction
	MembershipID string // "" = 일반 충전
	NewBalance   int64
}

// Charge — plan 기반 or 일반 충전.
//   - planID != nil: plan SELECT → is_active 검증 → transactions INSERT
//     → memberships INSERT (plan_snapshot 박제) → balance += plan.credit_amount
//   - planID == nil: transactions INSERT → balance += amount (memberships 없음)
//
// amount 는 planID 있으면 무시되고 plan.credit_amount 를 사용.
func (s *ChargeService) Charge(ctx context.Context, memberID string, planID *string, amount int64, createdBy string) (ChargeResult, error) {
	return s.ChargeWithMemo(ctx, memberID, planID, amount, "", createdBy)
}

// ChargeWithMemo preserves the original charge memo. Plan sale, wallet change
// and transaction row are committed atomically in the same canonical database.
func (s *ChargeService) ChargeWithMemo(ctx context.Context, memberID string, planID *string, amount int64, memo, createdBy string) (ChargeResult, error) {
	if planID == nil {
		if amount <= 0 {
			return ChargeResult{}, fmt.Errorf("%w: amount must be > 0", ErrInvalidInput)
		}
		result, err := s.wallet.Charge(ctx, memberID, amount, strings.TrimSpace(memo), createdBy)
		return fromLegacyWallet(result), mapWalletError(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ChargeResult{}, fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// SQLite BEGIN IMMEDIATE 등가: 첫 write 로 락 확보.
	// modernc/sqlite 는 BeginTx 시 기본 DEFERRED 이나 MaxOpenConns=1(v1 db.Open 정책)
	// 이므로 실질적으로 직렬화됨.
	q := dbgen.New(tx)

	m, err := q.GetMember(ctx, memberID)
	if errors.Is(err, sql.ErrNoRows) {
		return ChargeResult{}, ErrNotFound
	}
	if err != nil {
		return ChargeResult{}, fmt.Errorf("get member: %w", err)
	}
	if !m.IsActive {
		return ChargeResult{}, ErrInactive
	}

	var (
		credit   int64
		snapshot string
		planIDDB sql.NullString
	)

	if planID != nil {
		plan, gerr := q.GetPlan(ctx, *planID)
		if errors.Is(gerr, sql.ErrNoRows) {
			return ChargeResult{}, ErrNotFound
		}
		if gerr != nil {
			return ChargeResult{}, fmt.Errorf("get plan: %w", gerr)
		}
		if !plan.IsActive {
			return ChargeResult{}, ErrPlanInactive
		}
		credit = plan.CreditAmount
		// The original customer and transaction screens also consume this ledger.
		if strings.TrimSpace(memo) == "" {
			memo = plan.TierLabel + " · " + plan.PaymentMethod
		}
		snap, merr := marshalPlanSnapshot(plan)
		if merr != nil {
			return ChargeResult{}, merr
		}
		snapshot = snap
		planIDDB = sql.NullString{String: plan.ID, Valid: true}
	}

	if credit <= 0 || credit > math.MaxInt64-m.Balance {
		return ChargeResult{}, fmt.Errorf("%w: credit amount exceeds supported range", ErrInvalidInput)
	}
	newBalance := m.Balance + credit
	if err := q.AddToBalance(ctx, dbgen.AddToBalanceParams{
		Balance: credit,
		ID:      memberID,
	}); err != nil {
		return ChargeResult{}, fmt.Errorf("add to balance: %w", err)
	}

	t, err := q.InsertTransaction(ctx, dbgen.InsertTransactionParams{
		ID:           util.NewID(),
		MemberID:     memberID,
		Type:         "charge",
		Amount:       credit,
		BalanceAfter: newBalance,
		Memo:         nullStr(memo),
		CreatedBy:    nullStr(createdBy),
	})
	if err != nil {
		return ChargeResult{}, fmt.Errorf("insert tx: %w", err)
	}

	var membershipID string
	if planID != nil {
		mship, cerr := q.CreateMembership(ctx, dbgen.CreateMembershipParams{
			ID:           util.NewID(),
			MemberID:     memberID,
			PlanID:       planIDDB,
			PlanSnapshot: snapshot,
			ChargeTxID:   t.ID,
			CreatedBy:    nullStr(createdBy),
		})
		if cerr != nil {
			return ChargeResult{}, fmt.Errorf("create membership: %w", cerr)
		}
		membershipID = mship.ID
	}

	if err := tx.Commit(); err != nil {
		return ChargeResult{}, fmt.Errorf("commit: %w", err)
	}
	committed = true

	m.Balance = newBalance
	s.decryptMember(&m)
	return ChargeResult{
		Member:       m,
		Transaction:  t,
		MembershipID: membershipID,
		NewBalance:   newBalance,
	}, nil
}

// Deduct — 잔액 차감. 서비스명은 memo 로 기록.
func (s *ChargeService) Deduct(ctx context.Context, memberID string, amount int64, serviceName, createdBy string) (ChargeResult, error) {
	if amount <= 0 {
		return ChargeResult{}, fmt.Errorf("%w: amount must be > 0", ErrInvalidInput)
	}
	result, err := s.wallet.Deduct(ctx, memberID, amount, strings.TrimSpace(serviceName), createdBy)
	return fromLegacyWallet(result), mapWalletError(err)
}

func fromLegacyWallet(result legacy.WalletResult) ChargeResult {
	return ChargeResult{Member: dbgen.V2Member(result.Member), Transaction: dbgen.V2Transaction(result.Transaction), NewBalance: result.NewBalance}
}

func mapWalletError(err error) error {
	for _, pair := range [][2]error{{legacy.ErrNotFound, ErrNotFound}, {legacy.ErrInsufficient, ErrInsufficient}, {legacy.ErrInactive, ErrInactive}, {legacy.ErrInvalidInput, ErrInvalidInput}} {
		if errors.Is(err, pair[0]) {
			return fmt.Errorf("%w: %v", pair[1], err)
		}
	}
	return err
}

// RefundMembership — R5 대응. memberships.status='refunded' 로 전이하면서
// 잔액에서 plan_snapshot.credit_amount 만큼 되돌리고 refund 트랜잭션 기록.
// 이미 refunded/expired 이면 ErrMembershipDone.
//
// 정합성: BEGIN → GetMembership → parse snapshot →
// SubFromBalance → InsertTransaction(type=refund) → UpdateMembershipStatus.
func (s *ChargeService) RefundMembership(ctx context.Context, membershipID, createdBy string) (ChargeResult, error) {
	return s.transitionMembership(ctx, membershipID, "refunded", "refund", "환불", createdBy)
}

// ExpireMembership — 만료 전이. balance 롤백 없이 status='expired' 만.
// (만료는 이미 소진된 상태를 의미. balance 정산 필요 없음.)
func (s *ChargeService) ExpireMembership(ctx context.Context, membershipID, createdBy string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	q := dbgen.New(tx)

	ms, err := q.GetMembership(ctx, membershipID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get membership: %w", err)
	}
	if ms.Status != "active" {
		return ErrMembershipDone
	}
	if err := q.UpdateMembershipStatus(ctx, dbgen.UpdateMembershipStatusParams{
		Status: "expired",
		ID:     membershipID,
	}); err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	committed = true
	return nil
}

// transitionMembership — refund 전이의 공통 트랜잭션. balance 롤백 포함.
func (s *ChargeService) transitionMembership(ctx context.Context, membershipID, newStatus, txType, memoPrefix, createdBy string) (ChargeResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ChargeResult{}, fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	q := dbgen.New(tx)

	ms, err := q.GetMembership(ctx, membershipID)
	if errors.Is(err, sql.ErrNoRows) {
		return ChargeResult{}, ErrNotFound
	}
	if err != nil {
		return ChargeResult{}, fmt.Errorf("get membership: %w", err)
	}
	if ms.Status != "active" {
		return ChargeResult{}, ErrMembershipDone
	}

	// snapshot 에서 credit_amount 추출.
	snap := struct {
		CreditAmount int64  `json:"credit_amount"`
		TierLabel    string `json:"tier_label"`
	}{}
	if err := json.Unmarshal([]byte(ms.PlanSnapshot), &snap); err != nil {
		return ChargeResult{}, fmt.Errorf("parse snapshot: %w", err)
	}
	if snap.CreditAmount <= 0 {
		return ChargeResult{}, fmt.Errorf("%w: snapshot credit_amount invalid", ErrInvalidInput)
	}

	m, err := q.GetMember(ctx, ms.MemberID)
	if err != nil {
		return ChargeResult{}, fmt.Errorf("get member: %w", err)
	}
	newBalance := m.Balance - snap.CreditAmount
	if newBalance < 0 {
		return ChargeResult{}, fmt.Errorf("%w: refund exceeds balance", ErrInsufficient)
	}
	if err := q.SubFromBalance(ctx, dbgen.SubFromBalanceParams{
		Balance: snap.CreditAmount,
		ID:      ms.MemberID,
	}); err != nil {
		return ChargeResult{}, fmt.Errorf("sub balance: %w", err)
	}
	t, err := q.InsertTransaction(ctx, dbgen.InsertTransactionParams{
		ID:           util.NewID(),
		MemberID:     ms.MemberID,
		Type:         txType,
		Amount:       snap.CreditAmount,
		BalanceAfter: newBalance,
		Memo:         nullStr(memoPrefix + " · " + snap.TierLabel),
		CreatedBy:    nullStr(createdBy),
	})
	if err != nil {
		return ChargeResult{}, fmt.Errorf("insert refund tx: %w", err)
	}
	if err := q.UpdateMembershipStatus(ctx, dbgen.UpdateMembershipStatusParams{
		Status: newStatus,
		ID:     membershipID,
	}); err != nil {
		return ChargeResult{}, fmt.Errorf("update status: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ChargeResult{}, fmt.Errorf("commit: %w", err)
	}
	committed = true

	m.Balance = newBalance
	s.decryptMember(&m)
	return ChargeResult{
		Member:       m,
		Transaction:  t,
		MembershipID: membershipID,
		NewBalance:   newBalance,
	}, nil
}

// ─────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────

func (s *ChargeService) decryptMember(m *dbgen.V2Member) {
	if s.enc == nil {
		return
	}
	m.Name = s.enc.Decrypt(m.Name)
	m.Phone = s.enc.Decrypt(m.Phone)
	if m.Memo.Valid {
		m.Memo.String = s.enc.Decrypt(m.Memo.String)
	}
}

// marshalPlanSnapshot — pricing-model.md §4 스키마와 1:1 일치.
// perk_label 은 NULL 이면 JSON null 로 (빈 문자열 아님).
func marshalPlanSnapshot(p dbgen.V2MembershipPlan) (string, error) {
	var perk any
	if p.PerkLabel.Valid {
		perk = p.PerkLabel.String
	} else {
		perk = nil
	}
	m := map[string]any{
		"tier_label":     p.TierLabel,
		"tier_amount":    p.TierAmount,
		"payment_method": p.PaymentMethod,
		"price":          p.Price,
		"credit_amount":  p.CreditAmount,
		"perk_label":     perk,
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("marshal snapshot: %w", err)
	}
	return string(b), nil
}
