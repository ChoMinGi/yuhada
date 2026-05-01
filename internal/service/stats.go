package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mingicho/yuhada/internal/db/dbgen"
)

type StatsService struct {
	db *sql.DB
	q  *dbgen.Queries
}

// DashboardSnapshot — 대시보드 KPI 한 세트.
//
// Deduct/Charge Total 은 호출 시 전달한 Period 의 합계.
// Member/Cards 카운트는 period 와 무관 (현재 시점 스냅샷).
type DashboardSnapshot struct {
	DeductTotal    int64 // 선택된 기간의 차감 합
	ChargeTotal    int64 // 선택된 기간의 충전 합
	TotalLiability int64 // 활성 회원 잔액 합
	MemberCount    int64 // 활성 회원 수
	CardsIssued    int64
	CardsLost      int64
}

// Dashboard — period 범위의 합계 + 카운트 스냅샷을 한 번에 반환.
func (s *StatsService) Dashboard(ctx context.Context, p Period) (DashboardSnapshot, error) {
	snap := DashboardSnapshot{}
	start, end := p.Range(time.Now())

	var err error
	if snap.DeductTotal, err = sumAmount(ctx, s.db, "deduct", start, end); err != nil {
		return snap, fmt.Errorf("step=deduct: %w", err)
	}
	if snap.ChargeTotal, err = sumAmount(ctx, s.db, "charge", start, end); err != nil {
		return snap, fmt.Errorf("step=charge: %w", err)
	}
	if snap.TotalLiability, err = sumActiveBalance(ctx, s.db); err != nil {
		return snap, fmt.Errorf("step=total_liability: %w", err)
	}
	if snap.MemberCount, err = s.q.CountActiveMembers(ctx); err != nil {
		return snap, fmt.Errorf("step=member_count: %w", err)
	}
	if snap.CardsIssued, err = s.q.CountCardsIssued(ctx); err != nil {
		return snap, fmt.Errorf("step=cards_issued: %w", err)
	}
	if snap.CardsLost, err = s.q.CountCardsLost(ctx); err != nil {
		return snap, fmt.Errorf("step=cards_lost: %w", err)
	}
	return snap, nil
}

// ─────────────────────────────────────────────
// sqlc가 CAST 처리 못해서 direct SQL로 구현.
// ─────────────────────────────────────────────

func sumActiveBalance(ctx context.Context, db *sql.DB) (int64, error) {
	const q = "SELECT COALESCE(SUM(balance), 0) FROM members WHERE is_active = 1"
	var total sql.NullInt64
	if err := db.QueryRowContext(ctx, q).Scan(&total); err != nil {
		return 0, fmt.Errorf("sumActiveBalance: %w (sql=%q)", err, q)
	}
	return total.Int64, nil
}

func sumAmount(ctx context.Context, db *sql.DB, txType, start, end string) (int64, error) {
	const q = "SELECT COALESCE(SUM(amount), 0) FROM transactions WHERE type = ? AND created_at >= ? AND created_at < ?"
	var total sql.NullInt64
	if err := db.QueryRowContext(ctx, q, txType, start, end).Scan(&total); err != nil {
		return 0, fmt.Errorf("sumAmount(%s): %w (sql=%q)", txType, err, q)
	}
	return total.Int64, nil
}
