package service

import (
	"context"

	"github.com/mingicho/yuhada/internal/v2/db/dbgen"
)

// RecentActivity is the analytics dashboard's activity feed. Its exclusion is
// applied before LIMIT, while operational transaction history remains complete.
// MemberName stays encrypted, matching the existing dashboard renderer contract.
func (s *SalesService) RecentActivity(ctx context.Context, limit int) ([]dbgen.ListRecentTransactionsRow, error) {
	if limit < 1 || limit > 100 {
		limit = 8
	}
	clause, excluded := s.scope.Clause("t.member_id")
	args := append(append([]any{}, excluded...), limit)
	rows, err := s.db.QueryContext(ctx, `SELECT t.id,t.type,t.amount,t.memo,t.balance_after,t.created_at,m.id,m.name
		FROM transactions t JOIN members m ON m.id=t.member_id WHERE 1=1`+clause+` ORDER BY t.created_at DESC,t.id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []dbgen.ListRecentTransactionsRow{}
	for rows.Next() {
		var row dbgen.ListRecentTransactionsRow
		if err := rows.Scan(&row.TxID, &row.TxType, &row.TxAmount, &row.TxMemo, &row.TxBalanceAfter, &row.TxCreatedAt, &row.MemberID, &row.MemberName); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
