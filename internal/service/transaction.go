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
	now = now.UTC()
	endT := now.Add(time.Minute)
	var startT time.Time
	switch p {
	case PeriodToday:
		startT = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	case PeriodWeek:
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
	memberIDs []string // 내부 사용: 암호화 모드에서 Q → member ID 변환 결과
}

// normalizeType — 알 수 없는 값은 ""(전체)로.
func (f TxFilter) normalizedType() string {
	if f.Type == "charge" || f.Type == "deduct" {
		return f.Type
	}
	return ""
}

// resolveQ — 암호화 활성 + Q 비어있지 않으면, 이름/전화번호 검색을 member ID 목록으로 치환.
func (s *TransactionService) resolveQ(ctx context.Context, f *TxFilter) {
	if s.enc == nil || strings.TrimSpace(f.Q) == "" {
		return
	}
	members, err := s.q.ListAllMembers(ctx)
	if err != nil {
		return
	}
	q := strings.ToLower(strings.TrimSpace(f.Q))
	var ids []string
	for _, m := range members {
		name := strings.ToLower(s.enc.Decrypt(m.Name))
		phone := s.enc.Decrypt(m.Phone)
		if strings.Contains(name, q) || strings.Contains(phone, q) {
			ids = append(ids, m.ID)
		}
	}
	f.memberIDs = ids
	f.Q = "" // SQL LIKE 사용 안 함
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

	if len(f.memberIDs) > 0 {
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
	s.resolveQ(ctx, &f)

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
		// 회원명 복호화
		r.MemberName = s.enc.Decrypt(r.MemberName)
		out = append(out, r)
	}
	return out, rows.Err()
}

// SumsInPeriod — 기간 + 회원 검색 필터의 충전/차감 합계.
func (s *TransactionService) SumsInPeriod(ctx context.Context, f TxFilter) (deduct, charge int64, err error) {
	s.resolveQ(ctx, &f)

	scoped := TxFilter{Period: f.Period, memberIDs: f.memberIDs}
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
