package handler

import (
	"testing"
	"time"

	service "github.com/mingicho/yuhada/internal/v2/service"
)

func TestTrendRetainsEntireHistoryAndReconcilesBothSeries(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, kstLocSales())
	sales := []service.DailyBucket{{Date: "2026-05-01", Revenue: 100000}, {Date: "2026-11-15", Revenue: 200000}}
	usage := []service.DailyBucket{{Date: "2026-06-01", Revenue: 50000}, {Date: "2026-11-16", Revenue: 23000}}
	labels, a, b, g, rows := buildSalesTrend(sales, usage, "1970-01-01T00:00:00.000Z", "", "all", now)
	if g != "월별" || len(labels) != 7 || len(rows) != 7 || labels[0] != "2026.05" {
		t.Fatalf("lost early history: %v %s", labels, g)
	}
	var totalA, totalB int64
	for _, n := range a {
		totalA += n
	}
	for _, n := range b {
		totalB += n
	}
	if totalA != 300000 || totalB != 73000 || a[0] != 100000 {
		t.Fatalf("chart differs from period totals %d/%d", totalA, totalB)
	}
}

func TestTrendBoundaryAndDailyWeeklyGrain(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, kstLocSales())
	for _, tc := range []struct {
		since, until, grain string
		n                   int
	}{
		{"2026-10-31T15:00:00.000Z", "", "일별", 16},
		{"2026-10-31T15:00:00.000Z", "2026-11-02T15:00:00.000Z", "일별", 2},
		{"2026-09-30T15:00:00.000Z", "", "주별", 7},
		{"2026-12-01T15:00:00.000Z", "", "일별", 0},
	} {
		labels, _, _, g, _ := buildSalesTrend(nil, nil, tc.since, tc.until, "custom", now)
		if len(labels) != tc.n || g != tc.grain {
			t.Fatalf("%s..%s got%d/%s", tc.since, tc.until, len(labels), g)
		}
	}
	_, sold, used, _, _ := buildSalesTrend([]service.DailyBucket{{Date: "2026-11-01", Revenue: 1500}, {Date: "2026-11-02", Revenue: 2000}}, []service.DailyBucket{{Date: "2026-11-02", Revenue: 500}}, "2026-10-31T15:00:00.000Z", "2026-11-02T15:00:00.000Z", "custom", now)
	if len(sold) != 2 || sold[0] != 1500 || sold[1] != 2000 || used[0] != 0 || used[1] != 500 {
		t.Fatal("misaligned selected-day series")
	}
}
