package service

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/mingicho/yuhada/internal/crypto"
	"github.com/mingicho/yuhada/internal/db/dbgen"
)

type TransactionService struct {
	db  *sql.DB
	q   *dbgen.Queries
	enc *crypto.Enc
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
	// Displayed periods follow the salon's Korean calendar, independent of the
	// server/process timezone. Convert the selected midnight back to UTC only
	// when producing the canonical-ledger timestamp bounds.
	loc := time.FixedZone("KST", 9*60*60)
	now = now.In(loc)
	endT := now.Add(time.Minute)
	var startT time.Time
	switch p {
	case PeriodToday:
		startT = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	case PeriodWeek:
		dow := int(now.Weekday())
		if dow == 0 {
			dow = 7
		}
		startT = time.Date(now.Year(), now.Month(), now.Day()-dow+1, 0, 0, 0, 0, loc)
	case PeriodMonth:
		startT = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	case PeriodAll:
		startT = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	default:
		startT = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return startT.UTC().Format("2006-01-02T15:04:05.000Z"),
		endT.UTC().Format("2006-01-02T15:04:05.000Z")
}

// ListByMember — 특정 회원의 거래 내역.
func (s *TransactionService) ListByMember(ctx context.Context, memberID string) ([]dbgen.Transaction, error) {
	return s.q.ListTransactionsByMember(ctx, memberID)
}

// ListRecent — 대시보드용 최근 N건 (회원명 join).
func (s *TransactionService) ListRecent(ctx context.Context, limit int) ([]dbgen.ListRecentTransactionsRow, error) {
	rows, err := s.q.ListRecentTransactions(ctx)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	// 회원명 복호화
	for i := range rows {
		rows[i].MemberName = s.enc.Decrypt(rows[i].MemberName)
	}
	return rows, nil
}

// TxFilter — 거래 리스트/합계 공통 필터.
type TxFilter struct {
	Period    Period
	Type      string
	Q         string
	memberIDs []string // nil=unfiltered; non-nil empty=search resolved to no matches
}

// normalizeType — 알 수 없는 값은 ""(전체)로.
func (f TxFilter) normalizedType() string {
	if f.Type == "charge" || f.Type == "deduct" {
		return f.Type
	}
	return ""
}

// resolveQ — 암호화 활성 + Q 비어있지 않으면, 이름/전화번호 검색을 member ID 목록으로 치환.
func (s *TransactionService) resolveQ(ctx context.Context, f *TxFilter) error {
	if s.enc == nil || strings.TrimSpace(f.Q) == "" {
		return nil
	}
	// Resolve every member, rather than the membership screen's capped first
	// 500. Older customers must remain searchable in their transaction history.
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,phone FROM members`)
	if err != nil {
		return err
	}
	defer rows.Close()
	q := strings.ToLower(strings.TrimSpace(f.Q))
	ids := []string{}
	for rows.Next() {
		var id, encryptedName, encryptedPhone string
		if err := rows.Scan(&id, &encryptedName, &encryptedPhone); err != nil {
			return err
		}
		name := strings.ToLower(s.enc.Decrypt(encryptedName))
		phone := s.enc.Decrypt(encryptedPhone)
		if strings.Contains(name, q) || strings.Contains(phone, q) {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	f.memberIDs = ids
	f.Q = "" // SQL LIKE 사용 안 함
	return nil
}

// whereClause — 동적 WHERE 절과 args 빌드.
func (f TxFilter) whereClause() (string, []any) {
	start, end := f.Period.Range(time.Now())
	var sb strings.Builder
	sb.WriteString(" WHERE t.created_at >= ? AND t.created_at < ?")
	args := []any{start, end}

	if t := f.normalizedType(); t != "" {
		sb.WriteString(" AND t.type = ?")
		args = append(args, t)
	}

	if f.memberIDs != nil && len(f.memberIDs) == 0 {
		// A resolved search with no matches is an empty population, never All.
		sb.WriteString(" AND 0")
	} else if len(f.memberIDs) > 0 {
		// 암호화 모드: member ID 목록으로 필터
		placeholders := strings.Repeat("?,", len(f.memberIDs))
		sb.WriteString(" AND t.member_id IN (" + placeholders[:len(placeholders)-1] + ")")
		for _, id := range f.memberIDs {
			args = append(args, id)
		}
	} else if q := strings.TrimSpace(f.Q); q != "" {
		// 평문 모드: SQL LIKE
		sb.WriteString(" AND (m.name LIKE ? OR m.phone LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like)
	}

	return sb.String(), args
}

// ListInPeriod — 기간 + 타입 + 회원 검색 필터.
func (s *TransactionService) ListInPeriod(ctx context.Context, f TxFilter) ([]dbgen.ListTransactionsInPeriodRow, error) {
	if err := s.resolveQ(ctx, &f); err != nil {
		return nil, err
	}

	const base = `SELECT
	  t.id, t.type, t.amount, t.memo, t.balance_after, t.created_at,
	  m.id, m.name
	FROM transactions t
	JOIN members m ON m.id = t.member_id`

	where, args := f.whereClause()
	query := base + where + " ORDER BY t.created_at DESC,t.id DESC LIMIT 500"

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
		// 회원명 복호화
		r.MemberName = s.enc.Decrypt(r.MemberName)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListInPeriodPage exposes all matching history in bounded pages. The count
// and rows share a snapshot and identical filters; a page never redefines the
// population used by the full-history summary totals.
func (s *TransactionService) ListInPeriodPage(ctx context.Context, f TxFilter, limit, offset int) ([]dbgen.ListTransactionsInPeriodRow, int, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	if err := s.resolveQ(ctx, &f); err != nil {
		return nil, 0, err
	}
	where, args := f.whereClause()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var total int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM transactions t JOIN members m ON m.id=t.member_id`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	const base = `SELECT t.id,t.type,t.amount,t.memo,t.balance_after,t.created_at,m.id,m.name
		FROM transactions t JOIN members m ON m.id=t.member_id`
	pageArgs := append(append([]any{}, args...), limit, offset)
	rows, err := tx.QueryContext(ctx, base+where+` ORDER BY t.created_at DESC,t.id DESC LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		return nil, 0, err
	}
	out := []dbgen.ListTransactionsInPeriodRow{}
	for rows.Next() {
		var row dbgen.ListTransactionsInPeriodRow
		if err = rows.Scan(&row.TxID, &row.TxType, &row.TxAmount, &row.TxMemo, &row.TxBalanceAfter, &row.TxCreatedAt, &row.MemberID, &row.MemberName); err != nil {
			rows.Close()
			return nil, 0, err
		}
		row.MemberName = s.enc.Decrypt(row.MemberName)
		out = append(out, row)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	rows.Close()
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// SumsInPeriod — 기간 + 회원 검색 필터의 충전/차감 합계.
func (s *TransactionService) SumsInPeriod(ctx context.Context, f TxFilter) (deduct, charge int64, err error) {
	if err := s.resolveQ(ctx, &f); err != nil {
		return 0, 0, err
	}

	scoped := TxFilter{Period: f.Period, Q: f.Q, memberIDs: f.memberIDs}
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
