package handler

import (
	"fmt"
	"time"

	"github.com/mingicho/yuhada/internal/util"
	service "github.com/mingicho/yuhada/internal/v2/service"
	"github.com/mingicho/yuhada/internal/v2/view/page"
)

func buildSalesInsights(s service.SalesInsights, labels []string, sales, usage []int64, granularity string, rows []page.SalesTrendRow) page.SalesInsightsProps {
	p := page.SalesInsightsProps{
		UsageTotal: util.FormatKRW(s.UsageTotal), VisitorCount: fmt.Sprintf("%d명", s.VisitorCount), VisitCount: fmt.Sprintf("%d건", s.VisitCount),
		FirstCount: s.FirstSalesCount, RepeatCount: s.RepeatSalesCount, FirstAmount: util.FormatKRW(s.FirstSalesAmount), RepeatAmount: util.FormatKRW(s.RepeatSalesAmount),
		CurrentBalance: util.FormatKRW(s.CurrentBalance), CurrentMemberCount: s.CurrentMemberCount, LowBalanceCount: s.LowBalanceCount,
		TrendLabelsJSON: mustJSON(labels), TrendSalesJSON: mustJSON(sales), TrendUsageJSON: mustJSON(usage), TrendGranularity: granularity, TrendRows: rows,
	}
	if count := s.FirstSalesCount + s.RepeatSalesCount; count > 0 {
		p.RechargePercent = (s.RepeatSalesCount*100 + count/2) / count
		p.FirstPercent = 100 - p.RechargePercent
	}
	for _, b := range s.BalanceBuckets {
		percent := 0
		if s.CurrentMemberCount > 0 {
			percent = (b.MemberCount*100 + s.CurrentMemberCount/2) / s.CurrentMemberCount
		}
		p.BalanceBuckets = append(p.BalanceBuckets, page.SalesBalanceBucket{Label: b.Label, Count: b.MemberCount, Amount: util.FormatKRW(b.Balance), Percent: percent})
	}
	for _, m := range s.LowBalanceMembers {
		p.LowBalance = append(p.LowBalance, page.SalesBalanceMember{MemberID: m.MemberID, Name: m.Name, Balance: util.FormatKRW(m.Balance), LastVisit: formatKSTShort(m.LastVisitAt)})
	}
	return p
}

// buildSalesTrend retains every selected observation; wide ranges aggregate
// instead of silently dropping all but the latest sixty days.
func buildSalesTrend(sales, usage []service.DailyBucket, sinceUTC, untilUTC, period string, now time.Time) ([]string, []int64, []int64, string, []page.SalesTrendRow) {
	loc := kstLocSales()
	day := func(t time.Time) time.Time {
		t = t.In(loc)
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	}
	start, err := time.Parse(time.RFC3339Nano, sinceUTC)
	if err != nil {
		start = now
	}
	start = day(start)
	end := day(now)
	if untilUTC != "" {
		if u, e := time.Parse(time.RFC3339Nano, untilUTC); e == nil && day(u.Add(-time.Nanosecond)).Before(end) {
			end = day(u.Add(-time.Nanosecond))
		}
	}
	if period == "all" {
		start = end
		for _, series := range [][]service.DailyBucket{sales, usage} {
			for _, b := range series {
				if d, e := time.ParseInLocation("2006-01-02", b.Date, loc); e == nil && d.Before(start) {
					start = d
				}
			}
		}
	}
	labels := []string{}
	sold := []int64{}
	used := []int64{}
	rows := []page.SalesTrendRow{}
	if start.After(end) {
		return labels, sold, used, "일별", rows
	}
	span := int(end.Sub(start).Hours()/24) + 1
	granularity := "일별"
	step := 1
	if span > 120 {
		granularity = "월별"
	} else if span > 31 {
		granularity = "주별"
		step = 7
	}
	indexSales, indexUsage := map[string]int64{}, map[string]int64{}
	for _, b := range sales {
		indexSales[b.Date] += b.Revenue
	}
	for _, b := range usage {
		indexUsage[b.Date] += b.Revenue
	}
	for bucket := start; !bucket.After(end); {
		next := bucket.AddDate(0, 0, step)
		if granularity == "월별" {
			next = time.Date(bucket.Year(), bucket.Month()+1, 1, 0, 0, 0, 0, loc)
		}
		last := next.AddDate(0, 0, -1)
		if last.After(end) {
			last = end
		}
		var a, b int64
		for d := bucket; !d.After(last); d = d.AddDate(0, 0, 1) {
			key := d.Format("2006-01-02")
			a += indexSales[key]
			b += indexUsage[key]
		}
		label := bucket.Format("01/02")
		if granularity == "월별" {
			label = bucket.Format("2006.01")
		} else if granularity == "주별" {
			label += "–" + last.Format("01/02")
		}
		labels = append(labels, label)
		sold = append(sold, a)
		used = append(used, b)
		rows = append(rows, page.SalesTrendRow{Label: label, Sales: util.FormatKRW(a), Usage: util.FormatKRW(b)})
		bucket = next
	}
	return labels, sold, used, granularity, rows
}
