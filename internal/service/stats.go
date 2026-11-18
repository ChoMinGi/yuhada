package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mingicho/yuhada/internal/analytics"
)

type StatsService struct {
	db    *sql.DB
	scope analytics.Scope
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return snap, fmt.Errorf("dashboard begin: %w", err)
	}
	defer tx.Rollback()
	clause, excluded := s.scope.Clause("member_id")
	args := append([]any{start, end}, excluded...)
	if err = tx.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN type='deduct' THEN amount ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN type='charge' THEN amount ELSE 0 END),0)
		FROM transactions WHERE created_at>=? AND created_at<?`+clause, args...).Scan(&snap.DeductTotal, &snap.ChargeTotal); err != nil {
		return snap, fmt.Errorf("dashboard activity: %w", err)
	}
	clause, excluded = s.scope.Clause("id")
	if err = tx.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN is_active=1 THEN balance ELSE 0 END),0),
		COUNT(CASE WHEN is_active=1 THEN 1 END),
		COUNT(CASE WHEN card_uuid IS NOT NULL THEN 1 END),
		COUNT(CASE WHEN card_uuid IS NOT NULL AND is_active=0 THEN 1 END)
		FROM members WHERE 1=1`+clause, excluded...).Scan(&snap.TotalLiability, &snap.MemberCount, &snap.CardsIssued, &snap.CardsLost); err != nil {
		return snap, fmt.Errorf("dashboard members: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return snap, fmt.Errorf("dashboard finish: %w", err)
	}
	return snap, nil
}
