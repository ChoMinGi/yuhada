package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"time"

	"github.com/mingicho/yuhada/internal/crypto"
	"github.com/mingicho/yuhada/internal/v2/db/dbgen"
)

// SalesService — /v2/admin/sales 매출 대시보드 집계.
// SQL 은 row fetch 만 담당하고 aggregate 는 여기서 수행.
//
// Every canonical charge is a sale; recorded plan prices exclude bonus credit.
type SalesService struct {
	db  *sql.DB
	q   *dbgen.Queries
	enc *crypto.Enc
}

// ─────────────────────────────────────────────
// 결과 struct
// ─────────────────────────────────────────────

// SalesSummary — 상단 요약 카드 8개용 집계 (ERP 스타일).
// 4장 기본 + 4장 확장. 모두 fetchRange 한 번의 결과에서 도출.
type SalesSummary struct {
	// RealRevenue is charge amount, or the recorded plan price when present.
	RealRevenue int64
	// CreditTotal — 손님 잔액에 적립된 총액(부채) = 정액권 credit + 일반충전 amount.
	CreditTotal int64
	// BonusCost — 정액권 (credit − price) 합. 일반충전은 0.
	BonusCost int64
	// TxCount — 기간 내 charge 트랜잭션 건수 (정액권 + 일반).
	TxCount int
	// AvgTicket is total sale revenue divided by all charge transactions.
	AvgTicket int64
	// ActiveMembers — 기간 내 충전 트랜잭션이 있는 unique member_id 수.
	ActiveMembers int
	// PlanShareBP — 정액권 비율 (basis points, 0..10000). 표시 시 /100.
	// PlanShare(%) = PlanCount / TxCount * 100. TxCount=0 이면 0.
	PlanShareBP int
	// RepeatMembers — 기간 내 2건 이상 구매한 unique member_id 수.
	RepeatMembers int
}

// TierRow — 등급별 판매 리스트 한 줄.
// 일반 충전은 특수 row: TierLabel="일반 충전", PayMethod="".
type TierRow struct {
	TierLabel   string
	PayMethod   string
	TierAmount  int64 // 정렬 키. 일반 충전은 0.
	IsGeneric   bool  // 일반 충전 여부
	Count       int
	TotalPrice  int64
	TotalCredit int64
	TotalBonus  int64
}

// PayMethodRow — 결제수단 비율. 일반 충전은 unknown 그룹으로 포함한다.
type PayMethodRow struct {
	PayMethod  string // "card" | "cash" | "unknown"
	Count      int
	TotalPrice int64
}

// RecentSale — 최근 판매 이력 한 줄 (핸들러가 formatted 문자열 만들기 전 raw).
type RecentSale struct {
	TxID         string
	MemberID     string
	MemberName   string // 복호화 완료
	Amount       int64  // 판매 금액: 일반 charge amount 또는 plan price
	CreditAmount int64  // 실제 원장 적립액
	CreatedAt    string // ISO8601 UTC (핸들러에서 KST 로 포맷)
	// 정액권일 때만 채워진다. 비어 있으면 일반 충전.
	TierLabel string
	PayMethod string
	PerkLabel string
}

// ─────────────────────────────────────────────
// public API
// ─────────────────────────────────────────────
//
// since / until 은 ISO8601 UTC 사전순 비교 가능한 문자열.
// until == "" 이면 상한 없음 (지금까지).

// SalesSummary — 요약 카드 8개용. 한 번의 fetchRange 로 모든 지표 도출.
func (s *SalesService) SalesSummary(ctx context.Context, since, until string) (SalesSummary, error) {
	rows, err := s.fetchRange(ctx, since, until)
	if err != nil {
		return SalesSummary{}, err
	}
	out := SalesSummary{TxCount: len(rows)}
	// 활성/재구매 회원 계산용 카운터.
	perMember := map[string]int{}
	planCount := 0
	for _, r := range rows {
		if r.PlanSnapshot.Valid {
			snap := parseSnapshot(r.PlanSnapshot.String)
			out.RealRevenue += snap.Price
			out.CreditTotal += snap.CreditAmount
			out.BonusCost += snap.CreditAmount - snap.Price
			planCount++
		} else {
			// Existing, initial and generic charges are sales at their credited amount.
			out.RealRevenue += r.TxAmount
			out.CreditTotal += r.TxAmount
		}
		if r.MemberID != "" {
			perMember[r.MemberID]++
		}
	}
	if out.TxCount > 0 {
		out.AvgTicket = out.RealRevenue / int64(out.TxCount)
		out.PlanShareBP = planCount * 10000 / out.TxCount
	}
	out.ActiveMembers = len(perMember)
	for _, c := range perMember {
		if c >= 2 {
			out.RepeatMembers++
		}
	}
	return out, nil
}

// TopMember — 기간 내 판매 금액 상위 회원 한 명.
// TotalPrice includes generic/initial charges and recorded plan payment prices.
type TopMember struct {
	MemberID   string
	Name       string // 복호화 완료. 이름이 비어 있으면 "(삭제된 회원)".
	TotalPrice int64
	Count      int
}

// TopMembers — 기간 내 회원별 판매 금액 상위 limit 명.
// 이름은 fetchRange 결과의 mb.name (암호문) 을 복호화. limit ≤ 0 이면 전체.
func (s *SalesService) TopMembers(ctx context.Context, since, until string, limit int) ([]TopMember, error) {
	rows, err := s.fetchRange(ctx, since, until)
	if err != nil {
		return nil, err
	}
	type agg struct {
		total int64
		count int
		name  string // 첫 등장 시 세팅한 이름 (복호화 완료)
	}
	buckets := map[string]*agg{}
	for _, r := range rows {
		if r.MemberID == "" {
			continue
		}
		var price int64
		if r.PlanSnapshot.Valid {
			snap := parseSnapshot(r.PlanSnapshot.String)
			price = snap.Price
		} else {
			price = r.TxAmount
		}
		b := buckets[r.MemberID]
		if b == nil {
			name := ""
			if r.MemberName.Valid {
				if s.enc != nil {
					name = s.enc.Decrypt(r.MemberName.String)
				} else {
					name = r.MemberName.String
				}
			}
			if name == "" {
				name = "(삭제된 회원)"
			}
			b = &agg{name: name}
			buckets[r.MemberID] = b
		}
		b.total += price
		b.count++
	}
	out := make([]TopMember, 0, len(buckets))
	for id, b := range buckets {
		out = append(out, TopMember{
			MemberID:   id,
			Name:       b.name,
			TotalPrice: b.total,
			Count:      b.count,
		})
	}
	// TotalPrice desc, tie 는 count desc, 그래도 tie 면 name asc (안정 정렬).
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TotalPrice != out[j].TotalPrice {
			return out[i].TotalPrice > out[j].TotalPrice
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// SalesByTier — 등급 × 결제수단 그룹 집계 + 일반 충전 단일 row.
// 정렬: tier_amount asc, payment_method(card 먼저), 일반 충전은 항상 맨 끝.
func (s *SalesService) SalesByTier(ctx context.Context, since, until string) ([]TierRow, error) {
	rows, err := s.fetchRange(ctx, since, until)
	if err != nil {
		return nil, err
	}
	type key struct {
		TierAmount int64
		PayMethod  string
		TierLabel  string
	}
	buckets := map[key]*TierRow{}
	generic := TierRow{TierLabel: "일반 충전", IsGeneric: true}

	for _, r := range rows {
		if !r.PlanSnapshot.Valid {
			generic.Count++
			generic.TotalPrice += r.TxAmount
			generic.TotalCredit += r.TxAmount
			continue
		}
		snap := parseSnapshot(r.PlanSnapshot.String)
		k := key{TierAmount: snap.TierAmount, PayMethod: snap.PaymentMethod, TierLabel: snap.TierLabel}
		b := buckets[k]
		if b == nil {
			b = &TierRow{
				TierLabel:  snap.TierLabel,
				PayMethod:  snap.PaymentMethod,
				TierAmount: snap.TierAmount,
			}
			buckets[k] = b
		}
		b.Count++
		b.TotalPrice += snap.Price
		b.TotalCredit += snap.CreditAmount
		b.TotalBonus += snap.CreditAmount - snap.Price
	}

	out := make([]TierRow, 0, len(buckets)+1)
	for _, b := range buckets {
		out = append(out, *b)
	}
	// tier_amount asc, card 먼저(cash 뒤), label asc.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TierAmount != out[j].TierAmount {
			return out[i].TierAmount < out[j].TierAmount
		}
		if out[i].PayMethod != out[j].PayMethod {
			return payMethodRank(out[i].PayMethod) < payMethodRank(out[j].PayMethod)
		}
		return out[i].TierLabel < out[j].TierLabel
	})
	if generic.Count > 0 {
		out = append(out, generic)
	}
	return out, nil
}

// ExpiringMembership — 이번 달 만료 예정 정액권 한 건.
type ExpiringMembership struct {
	MembershipID string
	MemberID     string
	MemberName   string // 복호화 완료
	TierLabel    string // plan_snapshot.tier_label
	PayMethod    string // plan_snapshot.payment_method
	CreditAmount int64  // plan_snapshot.credit_amount
	ExpiresAt    string // UTC ISO8601 (핸들러에서 KST 로 포맷)
}

// ExpiringMemberships — since ≤ (purchased_at + 1년) < until 인 active memberships.
// 이번 달 시작/끝 UTC 를 인자로 받아 SQL에서 datetime(purchased_at, '+1 year') 비교.
func (s *SalesService) ExpiringMemberships(ctx context.Context, sinceUTC, untilUTC string) ([]ExpiringMembership, error) {
	rows, err := s.q.ListExpiringMemberships(ctx, dbgen.ListExpiringMembershipsParams{
		PurchasedAt:  sinceUTC,
		PurchasedAt2: untilUTC,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ExpiringMembership, 0, len(rows))
	for _, r := range rows {
		snap := parseSnapshot(r.PlanSnapshot)
		name := r.MemberName
		if s.enc != nil && name != "" {
			name = s.enc.Decrypt(name)
		}
		exp := ""
		if t, perr := time.Parse("2006-01-02T15:04:05.000Z", r.PurchasedAt); perr == nil {
			exp = t.AddDate(1, 0, 0).Format("2006-01-02T15:04:05.000Z")
		}
		out = append(out, ExpiringMembership{
			MembershipID: r.MembershipID,
			MemberID:     r.MemberID,
			MemberName:   name,
			TierLabel:    snap.TierLabel,
			PayMethod:    snap.PaymentMethod,
			CreditAmount: snap.CreditAmount,
			ExpiresAt:    exp,
		})
	}
	return out, nil
}

// DormantMember — 60일 이상 미사용 회원 한 명.
type DormantMember struct {
	MemberID string
	Name     string // 복호화 완료
	Balance  int64
	LastTxAt string // UTC ISO8601, 없으면 "" (한 번도 안 옴)
}

// DormantMembers — 마지막 tx 가 cutoffUTC 이전인 회원 (balance > 0, is_active).
func (s *SalesService) DormantMembers(ctx context.Context, cutoffUTC string) ([]DormantMember, error) {
	rows, err := s.q.ListDormantMembers(ctx, cutoffUTC)
	if err != nil {
		return nil, err
	}
	out := make([]DormantMember, 0, len(rows))
	for _, r := range rows {
		name := r.Name
		if s.enc != nil && name != "" {
			name = s.enc.Decrypt(name)
		}
		last := ""
		if r.LastTxAt != nil {
			if str, ok := r.LastTxAt.(string); ok {
				last = str
			}
		}
		out = append(out, DormantMember{
			MemberID: r.ID,
			Name:     name,
			Balance:  r.Balance,
			LastTxAt: last,
		})
	}
	return out, nil
}

// SalesByPayMethod includes every sale; generic charges have unknown payment method.
func (s *SalesService) SalesByPayMethod(ctx context.Context, since, until string) ([]PayMethodRow, error) {
	rows, err := s.fetchRange(ctx, since, until)
	if err != nil {
		return nil, err
	}
	buckets := map[string]*PayMethodRow{}
	for _, r := range rows {
		pm, price := "unknown", r.TxAmount
		if r.PlanSnapshot.Valid {
			snap := parseSnapshot(r.PlanSnapshot.String)
			price = snap.Price
			if snap.PaymentMethod != "" {
				pm = snap.PaymentMethod
			}
		}
		b := buckets[pm]
		if b == nil {
			b = &PayMethodRow{PayMethod: pm}
			buckets[pm] = b
		}
		b.Count++
		b.TotalPrice += price
	}

	out := make([]PayMethodRow, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, *b)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return payMethodRank(out[i].PayMethod) < payMethodRank(out[j].PayMethod)
	})
	return out, nil
}

// RecentSales — 최근 판매 이력 (최대 limit 건).
// 회원 이름은 여기서 복호화. until 이 지정되면 그 이전만.
func (s *SalesService) RecentSales(ctx context.Context, since, until string, limit int) ([]RecentSale, error) {
	rows, err := s.fetchRange(ctx, since, until)
	if err != nil {
		return nil, err
	}
	out := make([]RecentSale, 0, len(rows))
	for _, r := range rows {
		if until != "" && r.TxCreatedAt >= until {
			continue
		}
		name := ""
		if r.MemberName.Valid {
			if s.enc != nil {
				name = s.enc.Decrypt(r.MemberName.String)
			} else {
				name = r.MemberName.String
			}
		}
		if name == "" {
			name = "(삭제된 회원)"
		}
		item := RecentSale{
			TxID:         r.TxID,
			MemberID:     r.MemberID,
			MemberName:   name,
			Amount:       r.TxAmount,
			CreditAmount: r.TxAmount,
			CreatedAt:    r.TxCreatedAt,
		}
		if r.PlanSnapshot.Valid {
			snap := parseSnapshot(r.PlanSnapshot.String)
			item.Amount = snap.Price
			item.TierLabel = snap.TierLabel
			item.PayMethod = snap.PaymentMethod
			item.PerkLabel = snap.PerkLabel
		}
		out = append(out, item)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// ─────────────────────────────────────────────
// internal
// ─────────────────────────────────────────────

// DailyBucket — 그래프용 일자별 판매 금액 (price 기준).
type DailyBucket struct {
	Date    string // "MM-DD" 또는 "YYYY-MM-DD" (핸들러가 결정)
	Revenue int64  // 실제 매출 합
}

// DailyRevenue — since ≤ created_at < until 구간의 charge 를 일자(KST 기준) 별로 집계.
// 결측 일자는 handler 에서 채워 넣는다 (여기서는 실 판매 있는 날만 반환).
func (s *SalesService) DailyRevenue(ctx context.Context, since, until string) ([]DailyBucket, error) {
	rows, err := s.fetchRange(ctx, since, until)
	if err != nil {
		return nil, err
	}
	kst, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		kst = time.FixedZone("KST", 9*3600)
	}
	agg := map[string]int64{}
	for _, r := range rows {
		t, perr := time.Parse("2006-01-02T15:04:05.000Z", r.TxCreatedAt)
		if perr != nil {
			t, perr = time.Parse(time.RFC3339, r.TxCreatedAt)
			if perr != nil {
				continue
			}
		}
		day := t.In(kst).Format("2006-01-02")
		var price int64
		if r.PlanSnapshot.Valid {
			snap := parseSnapshot(r.PlanSnapshot.String)
			price = snap.Price
		} else {
			price = r.TxAmount
		}
		agg[day] += price
	}
	out := make([]DailyBucket, 0, len(agg))
	for day, rev := range agg {
		out = append(out, DailyBucket{Date: day, Revenue: rev})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

// fetchRange — since ≤ created_at < until (until 이 빈 문자열이면 상한 없음).
// SQL 은 항상 since 만 인자로 받고 until 은 Go 에서 필터.
// 트랜잭션 수가 소규모(하루~한 달 몇십 ~ 몇백건)라 in-memory 필터로 충분.
func (s *SalesService) fetchRange(ctx context.Context, since, until string) ([]dbgen.ListSalesInPeriodRow, error) {
	rows, err := s.q.ListSalesInPeriod(ctx, since)
	if err != nil {
		return nil, err
	}
	if until == "" {
		return rows, nil
	}
	filtered := rows[:0]
	for _, r := range rows {
		if r.TxCreatedAt < until {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

// planSnapshot — pricing-model.md §4 스키마 그대로.
// (handler.chargeSnapshot 와 동일 구조 · 별도 정의는 layering: service 는 handler 를 참조하지 않는다.)
type planSnapshot struct {
	TierLabel     string
	TierAmount    int64
	PaymentMethod string
	Price         int64
	CreditAmount  int64
	PerkLabel     string
}

func parseSnapshot(s string) planSnapshot {
	if s == "" {
		return planSnapshot{}
	}
	// perk_label 이 null 인 경우가 있어 raw 로 받아 안전 파싱.
	var raw map[string]any
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return planSnapshot{}
	}
	out := planSnapshot{}
	if v, ok := raw["tier_label"].(string); ok {
		out.TierLabel = v
	}
	if v, ok := raw["payment_method"].(string); ok {
		out.PaymentMethod = v
	}
	if v, ok := raw["tier_amount"].(float64); ok {
		out.TierAmount = int64(v)
	}
	if v, ok := raw["price"].(float64); ok {
		out.Price = int64(v)
	}
	if v, ok := raw["credit_amount"].(float64); ok {
		out.CreditAmount = int64(v)
	}
	if v, ok := raw["perk_label"].(string); ok {
		out.PerkLabel = v
	}
	return out
}

func payMethodRank(pm string) int {
	switch pm {
	case "card":
		return 0
	case "cash":
		return 1
	default:
		return 2
	}
}

// LedgerTotals includes every matching ledger row, independent of history display limits.
type LedgerTotals struct{ Charge, Deduct int64 }

func (s *SalesService) LedgerTotals(ctx context.Context, since, until string) (LedgerTotals, error) {
	var out LedgerTotals
	if until == "" {
		until = "9999-12-31T23:59:59.999Z"
	}
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN type='charge' THEN amount ELSE 0 END),0), COALESCE(SUM(CASE WHEN type IN ('deduct','refund') THEN amount ELSE 0 END),0) FROM transactions WHERE created_at >= ? AND created_at < ?`, since, until).Scan(&out.Charge, &out.Deduct)
	return out, err
}
