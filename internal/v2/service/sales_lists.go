package service

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type SalesListRow struct {
	ID, MemberID, Name, Detail, Date string
	Amount                           int64
	Count, Rank                      int
}

type SalesListPage struct {
	Rows               []SalesListRow
	Total, Page, Pages int
}

// ListDetails filters before paging. Names are encrypted, so name searches and
// grouped customer rankings require the complete matching period in memory.
// Only one bounded page is returned to the browser.
func (s *SalesService) ListDetails(ctx context.Context, kind, since, until, query string, number int, now time.Time) (SalesListPage, error) {
	all := []SalesListRow{}
	switch kind {
	case "top":
		members, err := s.TopMembers(ctx, since, until, 0)
		if err != nil {
			return SalesListPage{}, err
		}
		for i, m := range members {
			all = append(all, SalesListRow{ID: m.MemberID, MemberID: m.MemberID, Name: m.Name, Amount: m.TotalPrice, Count: m.Count, Rank: i + 1})
		}
	case "recharge":
		cutoff := now.UTC().AddDate(0, 0, -60).Format("2006-01-02T15:04:05.000Z")
		current := now.UTC().Format("2006-01-02T15:04:05.000Z")
		clause, excluded := s.scope.Clause("m.id")
		args := append([]any{cutoff, current}, excluded...)
		rows, err := s.db.QueryContext(ctx, `SELECT m.id,m.name,m.balance,MAX(t.created_at) FROM members m JOIN transactions t ON t.member_id=m.id AND t.type='deduct' AND t.created_at>=? AND t.created_at<=? WHERE m.is_active=1 AND m.balance<=50000`+clause+` GROUP BY m.id,m.name,m.balance ORDER BY m.balance ASC,MAX(t.created_at) DESC,m.id ASC`, args...)
		if err != nil {
			return SalesListPage{}, err
		}
		for rows.Next() {
			var row SalesListRow
			if err = rows.Scan(&row.MemberID, &row.Name, &row.Amount, &row.Date); err != nil {
				rows.Close()
				return SalesListPage{}, err
			}
			row.ID = row.MemberID
			row.Name = s.enc.Decrypt(row.Name)
			all = append(all, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return SalesListPage{}, err
		}
	case "recent":
		entries, err := s.RecentSales(ctx, since, until, 0)
		if err != nil {
			return SalesListPage{}, err
		}
		for _, r := range entries {
			detail := "충전"
			if r.TierLabel != "" {
				detail = r.TierLabel
			}
			all = append(all, SalesListRow{ID: r.TxID, MemberID: r.MemberID, Name: r.MemberName, Amount: r.Amount, Detail: detail, Date: r.CreatedAt})
		}
	default:
		return SalesListPage{}, fmt.Errorf("invalid sales list")
	}
	query = strings.ToLower(strings.TrimSpace(query))
	filtered := make([]SalesListRow, 0, len(all))
	for _, row := range all {
		if query == "" || strings.Contains(strings.ToLower(row.Name), query) {
			filtered = append(filtered, row)
		}
	}
	const size = 20
	pages := (len(filtered) + size - 1) / size
	if pages < 1 {
		pages = 1
	}
	if number < 1 {
		number = 1
	}
	if number > pages {
		number = pages
	}
	start := (number - 1) * size
	end := start + size
	if end > len(filtered) {
		end = len(filtered)
	}
	return SalesListPage{Rows: filtered[start:end], Total: len(filtered), Page: number, Pages: pages}, nil
}
