package service

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/mingicho/yuhada/internal/db/dbgen"
)

type TransactionService struct {
	db *sql.DB
	q  *dbgen.Queries
}

// Period — 기간 필터 값.
type Period string

const (
	PeriodToday Period = "today"
	PeriodWeek  Period = "week"
	PeriodMonth Period = "month"
	PeriodAll   Period = "all"
)

// Range — period를 (start, end) ISO8601 문자열로 변환.
func (p Period) Range(now time.Time) (start, end string) {
	now = now.UTC()
	endT := now.Add(time.Minute) // 약간의 여유
	var startT time.Time
	switch p {
	case PeriodToday:
		startT = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	case PeriodWeek:
		// 월요일 시작
		dow := int(now.Weekday())
		if dow == 0 {
			dow = 7
		}
		startT = time.Date(now.Year(), now.Month(), now.Day()-dow+1, 0, 0, 0, 0, time.UTC)
	case PeriodMonth:
		startT = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	case PeriodAll:
		startT = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	default:
		startT = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return startT.Format("2006-01-02T15:04:05.000Z"),
		endT.Format("2006-01-02T15:04:05.000Z")
}

// ListByMember — 특정 회원의 거래 내역.
func (s *TransactionService) ListByMember(ctx context.Context, memberID string) ([]dbgen.Transaction, error) {
	return s.q.ListTransactionsByMember(ctx, memberID)
}

// ListRecent — 대시보드용 최근 N건 (회원명 join).
// SQL에서 최대 20건 고정. 호출자가 필요하면 slice trim.
func (s *TransactionService) ListRecent(ctx context.Context, limit int) ([]dbgen.ListRecentTransactionsRow, error) {
	rows, err := s.q.ListRecentTransactions(ctx)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

// TxFilter — 거래 리스트/합계 공통 필터.
//
//   - Type ""              : 전체
//   - Type "charge"/"deduct": 해당 타입만
//   - Q   ""              : 회원 검색 없음
//   - Q   "최"/"010-"     : 회원명/전화번호 LIKE 검색
type TxFilter struct {
	Period Period
	Type   string
	Q      string
}

// normalizeType — 알 수 없는 값은 ""(전체)로.
func (f TxFilter) normalizedType() string {
	if f.Type == "charge" || f.Type == "deduct" {
		return f.Type
	}
	return ""
}

// whereClause — 동적 WHERE 절과 args 빌드.
// caller 가 base SELECT 뒤에 이걸 이어붙여서 사용.
func (f TxFilter) whereClause() (string, []any) {
	start, end := f.Period.Range(time.Now())
	var sb strings.Builder
	sb.WriteString(" WHERE t.created_at >= ? AND t.created_at < ?")
	args := []any{start, end}

	if t := f.normalizedType(); t != "" {
		sb.WriteString(" AND t.type = ?")
		args = append(args, t)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		sb.WriteString(" AND (m.name LIKE ? OR m.phone LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	return sb.String(), args
}

// ListInPeriod — 기간 + 타입 + 회원 검색 필터.
//
// sqlc 가 동적 WHERE 를 못 다루므로 direct SQL.
// row 타입은 sqlc 가 만든 ListTransactionsInPeriodRow 재사용.
func (s *TransactionService) ListInPeriod(ctx context.Context, f TxFilter) ([]dbgen.ListTransactionsInPeriodRow, error) {
	const base = `SELECT
	  t.id, t.type, t.amount, t.memo, t.balance_after, t.created_at,
	  m.id, m.name
	FROM transactions t
	JOIN members m ON m.id = t.member_id`

	where, args := f.whereClause()
	query := base + where + " ORDER BY t.created_at DESC LIMIT 500"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []dbgen.ListTransactionsInPeriodRow{}
	for rows.Next() {
		var r dbgen.ListTransactionsInPeriodRow
		if err := rows.Scan(
			&r.TxID, &r.TxType, &r.TxAmount, &r.TxMemo,
			&r.TxBalanceAfter, &r.TxCreatedAt,
			&r.MemberID, &r.MemberName,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SumsInPeriod — 기간 + 회원 검색 필터의 충전/차감 합계.
//
// Type 필터는 KPI 에 적용하지 않는다 — 사용자가 "차감만" 보고 있어도
// "오늘 충전 ₩…" 합계는 여전히 보여주는 게 의미 있음.
func (s *TransactionService) SumsInPeriod(ctx context.Context, f TxFilter) (deduct, charge int64, err error) {
	// Type 만 빼고 Period/Q 적용.
	scoped := TxFilter{Period: f.Period, Q: f.Q}
	where, args := scoped.whereClause()
	const base = `SELECT COALESCE(SUM(t.amount), 0)
	FROM transactions t
	JOIN members m ON m.id = t.member_id`

	var d, c sql.NullInt64
	if err := s.db.QueryRowContext(ctx, base+where+" AND t.type = 'deduct'", args...).Scan(&d); err != nil {
		return 0, 0, err
	}
	if err := s.db.QueryRowContext(ctx, base+where+" AND t.type = 'charge'", args...).Scan(&c); err != nil {
		return 0, 0, err
	}
	return d.Int64, c.Int64, nil
}
