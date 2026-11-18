package handler

import (
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	v2service "github.com/mingicho/yuhada/internal/v2/service"
)

func TestCohortOptionsAndPageLinksKeepIndependentWindows(t *testing.T) {
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		query           string
		months, cohorts int
	}{
		{"", 3, 6}, {"?months=3&cohorts=12", 3, 12}, {"?months=12&cohorts=3", 12, 3}, {"?months=999&cohorts=-1", 3, 6},
	} {
		got := retentionOptions(httptest.NewRequest("GET", "/v2/admin/retention"+test.query, nil), now)
		if got.Months != test.months || got.CohortMonths != test.cohorts {
			t.Fatalf("%s: %+v", test.query, got)
		}
	}
	p := buildRetentionProps(v2service.RetentionDashboard{AsOf: now, CohortMonths: 12, Months: make([]v2service.RetentionMonth, 3), DormantDays: 90, DormantPage: 2, DormantPages: 3})
	for _, link := range []string{p.PreviousURL, p.NextURL} {
		u, err := url.Parse(link)
		if err != nil || u.Query().Get("months") != "3" || u.Query().Get("cohorts") != "12" || u.Query().Get("absent") != "90" {
			t.Fatalf("lost query scopes: %s", link)
		}
	}
}
