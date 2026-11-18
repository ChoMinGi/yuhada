package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SalesInsights combines period activity with a separately labelled current
// balance snapshot. Membership status never hides canonical ledger activity.
type SalesInsights struct {
	UsageTotal         int64
	VisitorCount       int
	VisitCount         int
	FirstSalesCount    int
	RepeatSalesCount   int
	FirstSalesAmount   int64
	RepeatSalesAmount  int64
	DailyUsage         []DailyBucket
	CurrentBalance     int64
	CurrentMemberCount int
	BalanceBuckets     []BalanceBucket
	LowBalanceCount    int
	LowBalanceMembers  []LowBalanceMember
}

type BalanceBucket struct {
	Label       string
	MemberCount int
	Balance     int64
}

type LowBalanceMember struct {
	MemberID    string
	Name        string
	Balance     int64
	LastVisitAt string
}

// Insights performs SELECTs in one consistent transaction. Activity uses the
// supplied [since,until) interval. Current balances and low-balance candidates
// ignore that interval; candidates use the supplied now and its prior 60 days.
func (s *SalesService) Insights(ctx context.Context, since, until string, now time.Time) (SalesInsights, error) {
	out := SalesInsights{
		DailyUsage: []DailyBucket{}, LowBalanceMembers: []LowBalanceMember{},
		BalanceBuckets: []BalanceBucket{{Label: "0원"}, {Label: "5만원 이하"}, {Label: "5만 초과~10만원"}, {Label: "10만 초과~30만원"}, {Label: "30만원 초과"}},
	}
	if until == "" {
		until = "9999-12-31T23:59:59.999Z"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, fmt.Errorf("insights begin: %w", err)
	}
	defer tx.Rollback()
	clause, excluded := s.scope.Clause("member_id")
	args := append([]any{since, until}, excluded...)
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0),COUNT(DISTINCT member_id),COUNT(*) FROM transactions WHERE type='deduct' AND created_at>=? AND created_at<?`+clause, args...).Scan(&out.UsageTotal, &out.VisitorCount, &out.VisitCount); err != nil {
		return out, fmt.Errorf("insights usage: %w", err)
	}
	if err = s.insightsDaily(ctx, tx, since, until, &out); err != nil {
		return out, err
	}
	if err = s.insightsSales(ctx, tx, since, until, &out); err != nil {
		return out, err
	}
	if err = s.insightsBalances(ctx, tx, &out); err != nil {
		return out, err
	}
	cutoff := now.UTC().AddDate(0, 0, -60).Format("2006-01-02T15:04:05.000Z")
	current := now.UTC().Format("2006-01-02T15:04:05.000Z")
	clause, excluded = s.scope.Clause("m.id")
	args = append([]any{cutoff, current}, excluded...)
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM members m WHERE m.is_active=1 AND m.balance<=50000 AND EXISTS (SELECT 1 FROM transactions t WHERE t.member_id=m.id AND t.type='deduct' AND t.created_at>=? AND t.created_at<=?)`+clause, args...).Scan(&out.LowBalanceCount); err != nil {
		return out, fmt.Errorf("insights candidate count: %w", err)
	}
	if err = s.insightsCandidates(ctx, tx, cutoff, current, &out); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, fmt.Errorf("insights finish: %w", err)
	}
	return out, nil
}

func (s *SalesService) insightsDaily(ctx context.Context, tx *sql.Tx, since, until string, out *SalesInsights) error {
	clause, excluded := s.scope.Clause("member_id")
	args := append([]any{since, until}, excluded...)
	rows, err := tx.QueryContext(ctx, `SELECT strftime('%Y-%m-%d',created_at,'+9 hours'),SUM(amount) FROM transactions WHERE type='deduct' AND created_at>=? AND created_at<?`+clause+` GROUP BY strftime('%Y-%m-%d',created_at,'+9 hours') ORDER BY 1`, args...)
	if err != nil {
		return fmt.Errorf("insights daily usage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var day DailyBucket
		if err = rows.Scan(&day.Date, &day.Revenue); err != nil {
			return fmt.Errorf("insights daily usage scan: %w", err)
		}
		out.DailyUsage = append(out.DailyUsage, day)
	}
	return rows.Err()
}

func (s *SalesService) insightsSales(ctx context.Context, tx *sql.Tx, since, until string, out *SalesInsights) error {
	// The first sale is determined from all canonical charges, including charges
	// before this period. ID breaks equal timestamp ties without memo heuristics.
	clause, excluded := s.scope.Clause("t.member_id")
	args := append([]any{since, until}, excluded...)
	rows, err := tx.QueryContext(ctx, `SELECT t.amount,ms.plan_snapshot,NOT EXISTS (SELECT 1 FROM transactions older WHERE older.member_id=t.member_id AND older.type='charge' AND (older.created_at<t.created_at OR (older.created_at=t.created_at AND older.id<t.id))) FROM transactions t LEFT JOIN memberships ms ON ms.charge_tx_id=t.id WHERE t.type='charge' AND t.created_at>=? AND t.created_at<?`+clause, args...)
	if err != nil {
		return fmt.Errorf("insights first sales: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var amount int64
		var snapshot sql.NullString
		var first bool
		if err = rows.Scan(&amount, &snapshot, &first); err != nil {
			return fmt.Errorf("insights sale scan: %w", err)
		}
		if snapshot.Valid {
			amount = parseSnapshot(snapshot.String).Price
		}
		if first {
			out.FirstSalesCount++
			out.FirstSalesAmount += amount
		} else {
			out.RepeatSalesCount++
			out.RepeatSalesAmount += amount
		}
	}
	return rows.Err()
}

func (s *SalesService) insightsBalances(ctx context.Context, tx *sql.Tx, out *SalesInsights) error {
	clause, excluded := s.scope.Clause("id")
	rows, err := tx.QueryContext(ctx, `SELECT CASE WHEN balance=0 THEN 0 WHEN balance<=50000 THEN 1 WHEN balance<=100000 THEN 2 WHEN balance<=300000 THEN 3 ELSE 4 END,COUNT(*),SUM(balance) FROM members WHERE 1=1`+clause+` GROUP BY 1 ORDER BY 1`, excluded...)
	if err != nil {
		return fmt.Errorf("insights balances: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bucket, count int
		var balance int64
		if err = rows.Scan(&bucket, &count, &balance); err != nil {
			return fmt.Errorf("insights balance scan: %w", err)
		}
		out.BalanceBuckets[bucket].MemberCount = count
		out.BalanceBuckets[bucket].Balance = balance
		out.CurrentMemberCount += count
		out.CurrentBalance += balance
	}
	return rows.Err()
}

func (s *SalesService) insightsCandidates(ctx context.Context, tx *sql.Tx, cutoff, current string, out *SalesInsights) error {
	clause, excluded := s.scope.Clause("m.id")
	args := append([]any{cutoff, current}, excluded...)
	rows, err := tx.QueryContext(ctx, `SELECT m.id,m.name,m.balance,MAX(t.created_at) FROM members m JOIN transactions t ON t.member_id=m.id AND t.type='deduct' AND t.created_at>=? AND t.created_at<=? WHERE m.is_active=1 AND m.balance<=50000`+clause+` GROUP BY m.id,m.name,m.balance ORDER BY m.balance ASC,MAX(t.created_at) DESC,m.id ASC LIMIT 5`, args...)
	if err != nil {
		return fmt.Errorf("insights candidates: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var member LowBalanceMember
		if err = rows.Scan(&member.MemberID, &member.Name, &member.Balance, &member.LastVisitAt); err != nil {
			return fmt.Errorf("insights candidate scan: %w", err)
		}
		member.Name = s.enc.Decrypt(member.Name)
		out.LowBalanceMembers = append(out.LowBalanceMembers, member)
	}
	return rows.Err()
}
