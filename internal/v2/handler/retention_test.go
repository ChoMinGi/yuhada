package handler

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mingicho/yuhada/internal/v2/db/dbgen"
	v2service "github.com/mingicho/yuhada/internal/v2/service"
)

func TestRetentionRouteRequiresKakaoAndPreservesFilters(t *testing.T) {
	_, h, _, _ := handlerLedgerFixture(t)
	router := NewRouter(h.Deps)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/admin/retention?months=24&absent=90&page=2", nil))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("unauthed route status %d", w.Code)
	}
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil || location.Path != "/v2/login" || !strings.Contains(location.Query().Get("next"), "absent=90") {
		t.Fatalf("login return path %q: %v", w.Header().Get("Location"), err)
	}
}

func TestRetentionOptionsAndPaginationScope(t *testing.T) {
	now := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		query              string
		months, days, page int
	}{
		{"", 12, 60, 1},
		{"?months=24&absent=120&page=3", 24, 120, 3},
		{"?months=6&absent=90&page=-2", 6, 90, 1},
		{"?months=999&absent=1&page=overflow", 12, 60, 1},
	} {
		got := retentionOptions(httptest.NewRequest("GET", "/v2/admin/retention"+test.query, nil), now)
		if got.Months != test.months || got.DormantDays != test.days || got.Page != test.page || got.Now != now {
			t.Fatalf("%s: %+v", test.query, got)
		}
	}
	u, err := url.Parse(retentionURL(24, 90, 2))
	if err != nil || u.Query().Get("months") != "24" || u.Query().Get("absent") != "90" || u.Query().Get("page") != "2" {
		t.Fatalf("pagination scope %v %v", u, err)
	}
}

func TestRetentionPropsPreserveMissingAndImmatureValues(t *testing.T) {
	data := v2service.RetentionDashboard{
		AsOf:        time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC),
		Months:      []v2service.RetentionMonth{{Month: "2026-10", Visitors: 3, Returning: 1, MeanGapDays: 30, GapCount: 1}, {Month: "2026-11", Partial: true}},
		DormantDays: 90, DormantPage: 2, DormantPages: 3,
		Cohorts: []v2service.RetentionCohort{{Month: "2026-10", Size: 3, Cells: []v2service.RetentionCell{
			{Offset: 1, Status: "observing"}, {Offset: 2, Status: "future"}, {Offset: 3, Status: "complete", Returning: 0},
		}}},
	}
	p := buildRetentionProps(data)
	if p.CurrentRate != "—" || p.MeanGap != "—" || p.Rows[0].Rate != "33.3%" || p.Rows[1].Rate != "—" || p.Rows[1].Gap != "—" {
		t.Fatalf("missing values or ratios lost: %+v", p)
	}
	if p.Cohorts[0].Cells[0].Label != "관찰 중" || p.Cohorts[0].Cells[1].Label != "—" || p.Cohorts[0].Cells[2].Label != "0.0%" {
		t.Fatalf("maturity was converted to zero: %+v", p.Cohorts)
	}
	if !strings.Contains(p.PreviousURL, "absent=90") || !strings.Contains(p.NextURL, "page=3") || p.AsOf != "2026.11.17 21:00" {
		t.Fatalf("scope or KST display: %+v", p)
	}
}

func TestRetentionDashboardReadsWithoutWalletWritesOrSMS(t *testing.T) {
	db, h, _, sender := handlerLedgerFixture(t)
	member, err := h.Deps.Services.Member.Create(t.Context(), v2service.CreateMemberInput{Name: "Retention fixture", Phone: "01023456789", InitialCharge: 220000})
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range []string{"2026-01-01T00:00:00.000Z", "2026-02-01T00:00:00.000Z"} {
		if _, err = db.Exec(`INSERT INTO transactions(id,member_id,type,amount,balance_after,created_at) VALUES(?,?,'deduct',1000,0,?)`, at, member.ID, at); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.RetentionDashboard(w, httptest.NewRequest("GET", "/v2/admin/retention?months=24&absent=60", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("retention response %d %s", w.Code, w.Header())
	}
	for _, want := range []string{"월별 재방문", "평균 재방문 간격", "장기 미방문", "관찰 중", "Retention fixture", "/v2/admin/members/" + member.ID} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
	if sender.checks.Load() != 0 || len(sender.sent) != 0 {
		t.Fatal("a retention read invoked SMS")
	}
}

func TestPreviousInitialChargeIncludedWithoutGuessingPlan(t *testing.T) {
	rows := buildMemberChargeHistory([]dbgen.ListChargeHistoryWithPlanRow{{
		TxID: "old-charge", TxType: "charge", TxAmount: 220000, TxMemo: sql.NullString{String: "최초 충전", Valid: true}, TxCreatedAt: "2026-11-13T03:53:00.000Z",
	}, {
		TxID: "plan-charge", TxType: "charge", TxAmount: 240000, TxCreatedAt: "2026-11-14T03:53:00.000Z", PlanSnapshot: sql.NullString{String: `{"tier_label":"20만원 회원권","payment_method":"cash","price":200000,"credit_amount":240000}`, Valid: true}, MembershipStatus: sql.NullString{String: "active", Valid: true},
	}})
	if len(rows) != 2 || rows[0].TierLabel != "최초 충전" || rows[0].CreditAmount != "₩220,000" || rows[0].PayMethod != "" || rows[0].Status != "" {
		t.Fatalf("previous charge missing or guessed %+v", rows)
	}
	if rows[1].TierLabel != "20만원 회원권" || rows[1].PayMethod != "cash" || rows[1].CreditAmount != "₩240,000" {
		t.Fatalf("snapshot changed %+v", rows[1])
	}
}
