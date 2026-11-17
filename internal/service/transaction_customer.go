package service

import (
	"context"
	"database/sql"

	"github.com/mingicho/yuhada/internal/db/dbgen"
)

type CustomerActivity struct {
	HasTransactions bool
	LastCharge      *dbgen.Transaction
	LastVisit       *dbgen.Transaction
}

// LatestCustomerActivity reads the latest entry of each type across the full
// ledger. A limited mixed history cannot establish the last charge or visit.
func (s *TransactionService) LatestCustomerActivity(ctx context.Context, memberID string) (CustomerActivity, error) {
	out := CustomerActivity{}
	rows, err := s.db.QueryContext(ctx, `SELECT type,amount,memo,created_at FROM (
		SELECT type,amount,memo,created_at,ROW_NUMBER() OVER(PARTITION BY type ORDER BY created_at DESC,id DESC) position
		FROM transactions WHERE member_id=?
	) WHERE position=1`, memberID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry dbgen.Transaction
		var memo sql.NullString
		if err = rows.Scan(&entry.Type, &entry.Amount, &memo, &entry.CreatedAt); err != nil {
			return out, err
		}
		entry.Memo = memo
		out.HasTransactions = true
		switch entry.Type {
		case "charge":
			out.LastCharge = &entry
		case "deduct":
			out.LastVisit = &entry
		}
	}
	return out, rows.Err()
}
