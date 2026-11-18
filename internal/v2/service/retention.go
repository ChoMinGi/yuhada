package service

import (
	"context"
	"fmt"
	"sort"
	"time"
)

var retentionKST = time.FixedZone("KST", 9*60*60)

const retentionPageSize = 20

// RetentionOptions controls displayed history and the current dormant queue.
// A visit is a positive deduction on a distinct member/KST calendar day.
type RetentionOptions struct {
	Now         time.Time
	Months      int
	DormantDays int
	Page        int
}

type RetentionDashboard struct {
	AsOf                   time.Time
	HistoryStart           string
	LatestVisit            string
	Months                 []RetentionMonth
	MeanGapDays            float64
	GapCount               int
	ReturningCustomerCount int
	FirstVisitorCount      int
	DormantDays            int
	DormantCount           int
	NeverVisitedCount      int
	DormantPage            int
	DormantPages           int
	DormantMembers         []RetentionMember
	Cohorts                []RetentionCohort
}

type RetentionMonth struct {
	Month       string
	Visitors    int
	Returning   int
	VisitDays   int
	MeanGapDays float64
	GapCount    int
	Partial     bool
}

type RetentionMember struct {
	MemberID   string
	Name       string
	LastVisit  string
	Balance    int64
	DaysAbsent int
}

type RetentionCohort struct {
	Month string
	Size  int
	Cells []RetentionCell
}

type RetentionCell struct {
	Offset    int
	Returning int
	Status    string // complete, observing (current month), or future
}

type retentionMemberRecord struct {
	id, name string
	balance  int64
	active   bool
}

// Retention reads one consistent canonical-ledger snapshot. It never changes
// the ledger, creates visit records, sends messages, or interprets charges as
// visits. Historical aggregates retain inactive members; only the current
// dormant action queue is restricted to active members.
func (s *SalesService) Retention(ctx context.Context, opts RetentionOptions) (RetentionDashboard, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.Months != 3 && opts.Months != 6 && opts.Months != 12 && opts.Months != 24 {
		opts.Months = 3
	}
	if opts.DormantDays != 60 && opts.DormantDays != 90 && opts.DormantDays != 120 {
		opts.DormantDays = 60
	}
	if opts.Page < 1 {
		opts.Page = 1
	}
	now := opts.Now.In(retentionKST)
	currentMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, retentionKST)
	since := currentMonth.AddDate(0, 1-opts.Months, 0)
	out := RetentionDashboard{
		AsOf: now, DormantDays: opts.DormantDays,
		Months:         make([]RetentionMonth, opts.Months),
		DormantMembers: []RetentionMember{}, Cohorts: []RetentionCohort{},
	}
	for i := range out.Months {
		out.Months[i] = RetentionMonth{Month: since.AddDate(0, i, 0).Format("2006-01"), Partial: i == opts.Months-1}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, fmt.Errorf("retention begin: %w", err)
	}
	defer tx.Rollback()
	clause, excluded := s.scope.Clause("member_id")
	args := append([]any{opts.Now.UTC().Format("2006-01-02T15:04:05.000Z")}, excluded...)
	rows, err := tx.QueryContext(ctx, `SELECT member_id,strftime('%Y-%m-%d',created_at,'+9 hours') AS visit_day
		FROM transactions WHERE type='deduct' AND amount>0 AND created_at<=?
		`+clause+` GROUP BY member_id,visit_day ORDER BY member_id,visit_day`, args...)
	if err != nil {
		return out, fmt.Errorf("retention visits: %w", err)
	}
	visits := map[string][]time.Time{}
	for rows.Next() {
		var id, day string
		if err = rows.Scan(&id, &day); err != nil {
			rows.Close()
			return out, fmt.Errorf("retention visit scan: %w", err)
		}
		date, parseErr := time.ParseInLocation("2006-01-02", day, retentionKST)
		if parseErr != nil {
			rows.Close()
			return out, fmt.Errorf("retention visit date: %w", parseErr)
		}
		visits[id] = append(visits[id], date)
		if out.HistoryStart == "" || day < out.HistoryStart {
			out.HistoryStart = day
		}
		if day > out.LatestVisit {
			out.LatestVisit = day
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return out, fmt.Errorf("retention visits rows: %w", err)
	}
	rows.Close()
	clause, excluded = s.scope.Clause("id")
	rows, err = tx.QueryContext(ctx, `SELECT id,name,balance,is_active FROM members WHERE 1=1`+clause, excluded...)
	if err != nil {
		return out, fmt.Errorf("retention members: %w", err)
	}
	members := []retentionMemberRecord{}
	for rows.Next() {
		var member retentionMemberRecord
		if err = rows.Scan(&member.id, &member.name, &member.balance, &member.active); err != nil {
			rows.Close()
			return out, fmt.Errorf("retention member scan: %w", err)
		}
		members = append(members, member)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return out, fmt.Errorf("retention members rows: %w", err)
	}
	rows.Close()
	if err = tx.Commit(); err != nil {
		return out, fmt.Errorf("retention finish: %w", err)
	}

	monthVisitors := make([]map[string]bool, opts.Months)
	monthReturning := make([]map[string]bool, opts.Months)
	monthGapSum := make([]int, opts.Months)
	monthIndex := map[string]int{}
	for i, month := range out.Months {
		monthIndex[month.Month] = i
		monthVisitors[i], monthReturning[i] = map[string]bool{}, map[string]bool{}
	}
	cohortSizes := map[string]int{}
	cohortReturns := map[string]map[int]int{}
	returning := map[string]bool{}
	gapSum := 0
	for id, days := range visits {
		first := days[0]
		firstMonth := first.Format("2006-01")
		if !first.Before(since) {
			out.FirstVisitorCount++
			cohortSizes[firstMonth]++
		}
		seenMonths := map[int]bool{}
		for j, day := range days {
			if i, ok := monthIndex[day.Format("2006-01")]; ok {
				monthVisitors[i][id] = true
				out.Months[i].VisitDays++
				if j > 0 {
					monthReturning[i][id], returning[id] = true, true
					gap := int(day.Sub(days[j-1]).Hours() / 24)
					monthGapSum[i] += gap
					out.Months[i].GapCount++
					gapSum += gap
					out.GapCount++
				}
			}
			if !first.Before(since) {
				offset := (day.Year()-first.Year())*12 + int(day.Month()-first.Month())
				if offset >= 1 && offset <= 6 && !seenMonths[offset] {
					if cohortReturns[firstMonth] == nil {
						cohortReturns[firstMonth] = map[int]int{}
					}
					cohortReturns[firstMonth][offset]++
					seenMonths[offset] = true
				}
			}
		}
	}
	out.ReturningCustomerCount = len(returning)
	if out.GapCount > 0 {
		out.MeanGapDays = float64(gapSum) / float64(out.GapCount)
	}
	for i := range out.Months {
		out.Months[i].Visitors = len(monthVisitors[i])
		out.Months[i].Returning = len(monthReturning[i])
		if out.Months[i].GapCount > 0 {
			out.Months[i].MeanGapDays = float64(monthGapSum[i]) / float64(out.Months[i].GapCount)
		}
		if size := cohortSizes[out.Months[i].Month]; size > 0 {
			cohort := RetentionCohort{Month: out.Months[i].Month, Size: size, Cells: make([]RetentionCell, 6)}
			for j := range cohort.Cells {
				target := since.AddDate(0, i+j+1, 0)
				status := "complete"
				if target.Equal(currentMonth) {
					status = "observing"
				} else if target.After(currentMonth) {
					status = "future"
				}
				cohort.Cells[j] = RetentionCell{Offset: j + 1, Returning: cohortReturns[cohort.Month][j+1], Status: status}
			}
			out.Cohorts = append(out.Cohorts, cohort)
		}
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, retentionKST)
	nameByID := map[string]string{}
	dormant := []RetentionMember{}
	for _, member := range members {
		if !member.active {
			continue
		}
		days := visits[member.id]
		if len(days) == 0 {
			out.NeverVisitedCount++
			continue
		}
		last := days[len(days)-1]
		absent := int(today.Sub(last).Hours() / 24)
		if absent >= opts.DormantDays {
			dormant = append(dormant, RetentionMember{MemberID: member.id, LastVisit: last.Format("2006-01-02"), Balance: member.balance, DaysAbsent: absent})
			nameByID[member.id] = member.name
		}
	}
	sort.Slice(dormant, func(i, j int) bool {
		if dormant[i].DaysAbsent != dormant[j].DaysAbsent {
			return dormant[i].DaysAbsent > dormant[j].DaysAbsent
		}
		return dormant[i].MemberID < dormant[j].MemberID
	})
	out.DormantCount = len(dormant)
	out.DormantPages = (out.DormantCount + retentionPageSize - 1) / retentionPageSize
	if out.DormantPages < 1 {
		out.DormantPages = 1
	}
	out.DormantPage = opts.Page
	if out.DormantPage > out.DormantPages {
		out.DormantPage = out.DormantPages
	}
	start := (out.DormantPage - 1) * retentionPageSize
	end := start + retentionPageSize
	if end > len(dormant) {
		end = len(dormant)
	}
	for _, member := range dormant[start:end] {
		member.Name = s.enc.Decrypt(nameByID[member.MemberID])
		out.DormantMembers = append(out.DormantMembers, member)
	}
	return out, nil
}
