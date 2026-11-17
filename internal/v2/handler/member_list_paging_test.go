package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/mingicho/yuhada/internal/crypto"
	legacyservice "github.com/mingicho/yuhada/internal/service"
	v2service "github.com/mingicho/yuhada/internal/v2/service"
)

func memberPageFixture(t *testing.T, count int) *PageHandlers {
	t.Helper()
	db, h, _, _ := handlerLedgerFixture(t)
	enc, err := crypto.New([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	h.Deps.Services = v2service.New(db, enc)
	h.Deps.Legacy = legacyservice.New(db, enc)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < count; i++ {
		var card any
		if i < 30 {
			card = fmt.Sprintf("CARD-%03d", i)
		}
		_, err = tx.Exec(`INSERT INTO members(id,name,phone,balance,is_active,card_uuid,created_at) VALUES(?,?,?,?,?,?,?)`,
			fmt.Sprintf("list-%03d", i), enc.Encrypt(fmt.Sprintf("목록 고객 %03d", i)), enc.EncryptDeterministic(fmt.Sprintf("010%08d", i)), i*1000, i%3 != 0, card, "2026-05-01T00:00:00.000Z")
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	return h
}

func listHTML(t *testing.T, handler http.HandlerFunc, path string, htmx bool) string {
	t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	if htmx {
		r.Header.Set("HX-Request", "true")
	}
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("list response %d %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

func listedIDs(t *testing.T, html, attribute string) []string {
	t.Helper()
	matches := regexp.MustCompile(attribute+`="([^"]+)"`).FindAllStringSubmatch(html, -1)
	counts := map[string]int{}
	ids := []string{}
	for _, match := range matches {
		if counts[match[1]] == 0 {
			ids = append(ids, match[1])
		}
		counts[match[1]]++
	}
	for id, count := range counts {
		if count != 2 {
			t.Fatalf("%s must appear once per responsive view, got %d", id, count)
		}
	}
	return ids
}

func TestMembersPagesFiltersAndAppendKeepEveryMemberOnce(t *testing.T) {
	h := memberPageFixture(t, 45)
	seen := map[string]bool{}
	for number, want := range []int{20, 20, 5} {
		html := listHTML(t, h.MembersList, fmt.Sprintf("/v2/admin/members?page=%d", number+1), false)
		ids := listedIDs(t, html, "data-member-id")
		if len(ids) != want || !strings.Contains(html, "총 45명") {
			t.Fatalf("page%d size/count incorrect: %v", number+1, ids)
		}
		for _, id := range ids {
			if seen[id] {
				t.Fatalf("member repeated across pages: %s", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != 45 {
		t.Fatalf("members lost: %d", len(seen))
	}
	first := listHTML(t, h.MembersList, "/v2/admin/members?status=active&sort=balance_asc", false)
	ids := listedIDs(t, first, "data-member-id")
	if len(ids) != 20 || ids[0] != "list-001" || !strings.Contains(first, "총 30명") || strings.Contains(first, `name="page"`) {
		t.Fatal("member filters/sort/count or reset-on-filter contract incorrect")
	}
	for _, expected := range []string{`hx-sync="#members-list:replace"`, `href="/v2/admin/members/list-001"`, `status=active`, `sort=balance_asc`} {
		if !strings.Contains(first, expected) {
			t.Errorf("missing filter/keyboard support %q", expected)
		}
	}
	appendHTML := listHTML(t, h.MembersList, "/v2/admin/members?page=2&more=1&start=1", true)
	ids = listedIDs(t, appendHTML, "data-member-id")
	if len(ids) != 20 || ids[0] != "list-024" || !strings.Contains(appendHTML, "1–40명 표시") || strings.Contains(appendHTML, "<html") {
		t.Fatal("load-more returned a cumulative prefix or lost displayed range")
	}
	for _, expected := range []string{`hx-swap-oob="beforeend:#members-desktop"`, `hx-swap-oob="beforeend:#members-mobile"`, `start=1`, `hx-push-url="false"`, `hx-sync="#members-list:drop"`} {
		if !strings.Contains(appendHTML, expected) {
			t.Errorf("missing append contract %q", expected)
		}
	}
	last := listHTML(t, h.MembersList, "/v2/admin/members?page=3&more=1&start=2", true)
	if len(listedIDs(t, last, "data-member-id")) != 5 || !strings.Contains(last, "21–45명 표시") || strings.Contains(last, "더 보기") {
		t.Fatal("direct page2 followed by more has incorrect range/end state")
	}
	empty := listHTML(t, h.MembersList, "/v2/admin/members?filter=1&q=NO_SUCH_MEMBER", true)
	if len(listedIDs(t, empty, "data-member-id")) != 0 || !strings.Contains(empty, "총 0명 · 0–0명 표시") {
		t.Fatal("empty member search invented a population")
	}
}

func TestCardsUnifiedPagesStatusesSearchAndWholePopulationSummary(t *testing.T) {
	h := memberPageFixture(t, 45)
	seen := map[string]bool{}
	for number, want := range []int{20, 20, 5} {
		html := listHTML(t, h.CardsList, fmt.Sprintf("/v2/admin/cards?page=%d", number+1), false)
		ids := listedIDs(t, html, "data-card-member-id")
		if len(ids) != want {
			t.Fatalf("card page%d count %d", number+1, len(ids))
		}
		for _, id := range ids {
			if seen[id] {
				t.Fatalf("card row repeated %s", id)
			}
			seen[id] = true
		}
		if strings.Contains(html, ">발급일<") || !strings.Contains(html, ">가입일<") {
			t.Fatal("card page invented an issuance date")
		}
	}
	if len(seen) != 45 {
		t.Fatal("issued/unissued/inactive members were silently omitted")
	}
	for status, want := range map[string]int{"active": 20, "lost": 10, "unissued": 15} {
		html := listHTML(t, h.CardsList, "/v2/admin/cards?status="+status, false)
		if len(listedIDs(t, html, "data-card-member-id")) != want || !strings.Contains(html, "전체 회원 기준") {
			t.Fatalf("status%s count incorrect", status)
		}
	}
	query := url.Values{"q": {"card-001"}, "status": {"active"}}
	matched := listHTML(t, h.CardsList, "/v2/admin/cards?"+query.Encode(), false)
	ids := listedIDs(t, matched, "data-card-member-id")
	if len(ids) != 1 || ids[0] != "list-001" || !strings.Contains(matched, "총 1명") {
		t.Fatal("card UUID filter did not match the complete population")
	}
	more := listHTML(t, h.CardsList, "/v2/admin/cards?more=1&page=2&start=1", true)
	if len(listedIDs(t, more, "data-card-member-id")) != 20 || !strings.Contains(more, "1–40명 표시") || !strings.Contains(more, `hx-swap-oob="beforeend:#cards-desktop"`) {
		t.Fatal("card load more did not append one exact page")
	}
}

func TestBothEncryptedMemberSearchesIncludeHistoryBeyondFiveHundred(t *testing.T) {
	h := memberPageFixture(t, 505)
	ctx := t.Context()
	for name, search := range map[string]func() (int, error){
		"v2": func() (int, error) {
			rows, err := h.Deps.Services.Member.Search(ctx, "목록 고객 000", v2service.SortDefault)
			return len(rows), err
		},
		"canonical": func() (int, error) {
			rows, err := h.legacyServices().Member.Search(ctx, "목록 고객 000", legacyservice.SortDefault)
			return len(rows), err
		},
	} {
		count, err := search()
		if err != nil || count != 1 {
			t.Fatalf("%s old encrypted member hidden: count%d error%v", name, count, err)
		}
	}
	html := listHTML(t, h.MembersList, "/v2/admin/members?q="+url.QueryEscape("목록 고객 000"), false)
	ids := listedIDs(t, html, "data-member-id")
	if len(ids) != 1 || ids[0] != "list-000" {
		t.Fatal("listing still searches only first500 members")
	}
	cards := listHTML(t, h.CardsList, "/v2/admin/cards?q=CARD-000", false)
	if ids = listedIDs(t, cards, "data-card-member-id"); len(ids) != 1 || ids[0] != "list-000" {
		t.Fatal("card search still searches only first500 members")
	}
}
