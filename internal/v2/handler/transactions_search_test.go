package handler

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	v2service "github.com/mingicho/yuhada/internal/v2/service"
)

func TestTransactionSearchUpdatesRowsTotalsAndFilterLinksTogether(t *testing.T) {
	_, h, _, _ := handlerLedgerFixture(t)
	ctx := t.Context()
	member, err := h.Deps.Services.Member.Create(ctx, v2service.CreateMemberInput{Name: "검색 고객", Phone: "01000000001", InitialCharge: 100000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.Deps.Services.Member.Create(ctx, v2service.CreateMemberInput{Name: "다른 고객", Phone: "01000000002", InitialCharge: 300000}); err != nil {
		t.Fatal(err)
	}
	if _, err = h.Deps.Services.Charge.Deduct(ctx, member.ID, 20000, "검색 시술", "fixture"); err != nil {
		t.Fatal(err)
	}
	request := func(q string) string {
		r := httptest.NewRequest("GET", "/v2/admin/transactions?"+url.Values{"period": {"all"}, "q": {q}}.Encode(), nil)
		r.Header.Set("HX-Request", "true")
		r.Header.Set("HX-Trigger-Name", "q")
		w := httptest.NewRecorder()
		h.TransactionsPage(w, r)
		if w.Code != 200 {
			t.Fatalf("search returned %d: %s", w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	html := request("검색 고객")
	for _, want := range []string{
		"검색 고객", "검색 시술", "₩100,000", "₩20,000",
		`id="tx-summary" hx-swap-oob="outerHTML"`, `id="tx-filter-tabs" hx-swap-oob="outerHTML"`,
		`period=month&amp;q=%EA%B2%80%EC%83%89+%EA%B3%A0%EA%B0%9D`,
		`period=all&amp;q=%EA%B2%80%EC%83%89+%EA%B3%A0%EA%B0%9D&amp;type=charge`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("searched response missing %q", want)
		}
	}
	for _, excluded := range []string{"다른 고객", "₩300,000", "<html", `id="tx-search"`} {
		if strings.Contains(html, excluded) {
			t.Errorf("search response leaks unrelated data or replaces focus input: %q", excluded)
		}
	}
	empty := request("없는 고객")
	if !strings.Contains(empty, "거래 내역이 없습니다") || strings.Count(empty, "₩0") != 2 || !strings.Contains(empty, `hx-swap-oob="outerHTML"`) {
		t.Fatal("empty search must clear results and both totals together")
	}
}

func TestTransactionMoreAppendsOnlyNextPageAndKeepsFullFilteredTotals(t *testing.T) {
	db, h, _, _ := handlerLedgerFixture(t)
	member, err := h.Deps.Services.Member.Create(t.Context(), v2service.CreateMemberInput{Name: "오래된 고객", Phone: "01000000003"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 53; i++ {
		if _, err = db.Exec(`INSERT INTO transactions(id,member_id,type,amount,balance_after,memo,created_at) VALUES(?,?,'deduct',1000,0,?,'2026-11-01T00:00:00.000Z')`, fmt.Sprintf("fixture-row-%03d", i), member.ID, fmt.Sprintf("audit-row-%03d", i)); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	rowPattern := regexp.MustCompile(`audit-row-\d{3}`)
	for number, count := range []int{20, 20, 13} {
		query := url.Values{"period": {"all"}, "type": {"deduct"}, "q": {member.Name}, "page": {fmt.Sprint(number + 1)}}
		if number > 0 {
			query.Set("more", "1")
			query.Set("start", "1")
		}
		r := httptest.NewRequest("GET", "/v2/admin/transactions?"+query.Encode(), nil)
		if number > 0 {
			r.Header.Set("HX-Request", "true")
		}
		w := httptest.NewRecorder()
		h.TransactionsPage(w, r)
		if w.Code != 200 {
			t.Fatalf("page%d returned%d", number+1, w.Code)
		}
		html := w.Body.String()
		matches := rowPattern.FindAllString(html, -1)
		if len(matches) != count {
			t.Fatalf("page%d has%d rows, want%d", number+1, len(matches), count)
		}
		for _, value := range matches {
			if seen[value] {
				t.Fatalf("duplicate across pages: %s", value)
			}
			seen[value] = true
		}
		if !strings.Contains(html, "총 53건") {
			t.Fatal("paging total must cover full filtered ledger")
		}
		if strings.Contains(html, "@component.") || strings.Contains(html, "@txTypeTabs") {
			t.Fatal("template component text leaked to HTML")
		}
		if number == 0 && !strings.Contains(html, "₩53,000") {
			t.Fatal("summary must cover all53 records, not first20")
		}
		if number > 0 && (!strings.Contains(html, `id="transaction-rows" hx-swap-oob="beforeend"`) || strings.Contains(html, "<html")) {
			t.Fatal("more must append next rows rather than insert full page")
		}
		if number == 2 && strings.Contains(html, "더 보기") {
			t.Fatal("last page must not offer more")
		}
	}
	if len(seen) != 53 {
		t.Fatalf("lost history: %d/53", len(seen))
	}
}
