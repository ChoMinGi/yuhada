package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mingicho/yuhada/internal/service"
	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/v2/view/page"
)

// Both frontends use the existing business services and canonical ledger.
func (h *PageHandlers) legacyServices() *service.Services {
	if h.Deps.Legacy != nil {
		return h.Deps.Legacy
	}
	return service.New(h.Deps.Services.DB, h.Deps.Services.Enc, h.Deps.Services.AnalyticsExcludedMemberIDs...)
}

func (h *PageHandlers) CardsList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	all, err := h.legacyServices().Member.Search(r.Context(), "", service.SortDefault)
	if err != nil {
		http.Error(w, "카드 목록을 불러오지 못했습니다", 500)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	status := r.URL.Query().Get("status")
	if status != "active" && status != "lost" && status != "unissued" {
		status = "all"
	}
	d := page.CardsData{Query: query, Status: status}
	filtered := make([]page.CardRow, 0, len(all))
	for _, m := range all {
		state := "unissued"
		if m.CardUuid.Valid && m.CardUuid.String != "" {
			d.Issued++
			if m.IsActive {
				d.Active++
				state = "active"
			} else {
				d.Lost++
				state = "lost"
			}
		} else {
			d.UnissuedCount++
		}
		if status != "all" && status != state {
			continue
		}
		q := strings.ToLower(query)
		phoneQuery := phoneListQuery(query)
		if q != "" && !strings.Contains(strings.ToLower(m.Name), q) && !strings.Contains(strings.ToLower(m.CardUuid.String), q) && !strings.Contains(m.Phone, q) && (phoneQuery == "" || !strings.Contains(m.Phone, phoneQuery)) {
			continue
		}
		filtered = append(filtered, page.CardRow{MemberID: m.ID, Name: m.Name, Phone: util.FormatPhone(m.Phone), Card: m.CardUuid.String, JoinedAt: joinedDate(m.CreatedAt), Active: m.IsActive, Status: state, Balance: util.FormatKRW(m.Balance)})
	}
	start, end, paging := listPaging(r, len(filtered), "/v2/admin/cards", "cards-list", "cards-paging", url.Values{"q": {query}, "status": {status}})
	d.Cards, d.Paging = filtered[start:end], paging
	if isHTMX(r) && r.URL.Query().Get("more") == "1" {
		render(w, r, 200, page.CardsAppend(d))
		return
	}
	if isHTMX(r) && r.URL.Query().Get("filter") == "1" {
		render(w, r, 200, page.CardsResults(d))
		return
	}
	render(w, r, 200, page.Cards(d))
}

func (h *PageHandlers) CardIssue(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.loadMemberDetail(w, r); !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "잘못된 입력입니다", 400)
		return
	}
	card := strings.TrimSpace(r.FormValue("card"))
	if card == "" {
		card = util.NewCardUUID()
	}
	if err := h.legacyServices().Member.IssueCard(r.Context(), chi.URLParam(r, "id"), card); err != nil {
		http.Error(w, "카드 발급에 실패했습니다. 중복 발급 여부를 확인해 주세요.", 422)
		return
	}
	if m, ok := h.loadMemberDetail(w, r); ok {
		render(w, r, 200, page.MemberCardRow(m))
	}
}

func (h *PageHandlers) CardLost(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.loadMemberDetail(w, r); !ok {
		return
	}
	if err := h.legacyServices().Member.ReportLost(r.Context(), chi.URLParam(r, "id")); err != nil {
		http.Error(w, "분실 처리를 완료하지 못했습니다", 500)
		return
	}
	hxRedirect(w, "/v2/admin/members/"+chi.URLParam(r, "id"))
}

func (h *PageHandlers) CardReactivate(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.loadMemberDetail(w, r); !ok {
		return
	}
	if err := h.legacyServices().Member.Reactivate(r.Context(), chi.URLParam(r, "id")); err != nil {
		http.Error(w, "재활성화를 완료하지 못했습니다", 500)
		return
	}
	hxRedirect(w, "/v2/admin/members/"+chi.URLParam(r, "id"))
}

func (h *PageHandlers) LostModal(w http.ResponseWriter, r *http.Request) {
	if m, ok := h.loadMemberDetail(w, r); ok {
		render(w, r, 200, page.LostModal(m))
	}
}
func (h *PageHandlers) ReactivateModal(w http.ResponseWriter, r *http.Request) {
	if m, ok := h.loadMemberDetail(w, r); ok {
		render(w, r, 200, page.ReactivateModal(m))
	}
}
func (h *PageHandlers) DeleteModal(w http.ResponseWriter, r *http.Request) {
	if m, ok := h.loadMemberDetail(w, r); ok {
		render(w, r, 200, page.DeleteModal(m))
	}
}

func (h *PageHandlers) TransactionsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	p := service.Period(r.URL.Query().Get("period"))
	switch p {
	case "this-week":
		p = service.PeriodWeek
	case "this-month":
		p = service.PeriodMonth
	case service.PeriodToday, service.PeriodWeek, service.PeriodMonth, service.PeriodAll:
	default:
		p = service.PeriodToday
	}
	typeFilter := r.URL.Query().Get("type")
	if typeFilter != "charge" && typeFilter != "deduct" {
		typeFilter = ""
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	filter := service.TxFilter{Period: p, Type: typeFilter, Q: q}
	pageNumber, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if pageNumber < 1 || pageNumber > int(^uint(0)>>1)/memberListPageSize {
		pageNumber = 1
	}
	offset := (pageNumber - 1) * memberListPageSize
	rows, total, err := h.legacyServices().Tx.ListInPeriodPage(r.Context(), filter, memberListPageSize, offset)
	if err != nil {
		http.Error(w, "거래 내역을 불러오지 못했습니다", 500)
		return
	}
	start, _, paging := listPaging(r, total, "/v2/admin/transactions", "tx-list", "tx-paging", url.Values{"period": {string(p)}, "type": {typeFilter}, "q": {q}})
	paging.Unit = "건"
	if start != offset {
		rows, _, err = h.legacyServices().Tx.ListInPeriodPage(r.Context(), filter, memberListPageSize, start)
		if err != nil {
			http.Error(w, "거래 내역을 불러오지 못했습니다", 500)
			return
		}
	}
	views := make([]page.TxRow, len(rows))
	for i, row := range rows {
		sign := "-"
		if row.TxType == "charge" {
			sign = "+"
		}
		memo := ""
		if row.TxMemo.Valid {
			memo = row.TxMemo.String
		}
		views[i] = page.TxRow{ID: row.TxID, Type: row.TxType, MemberID: row.MemberID, MemberName: row.MemberName, Memo: memo, Amount: sign + util.FormatKRW(row.TxAmount), At: formatKST(row.TxCreatedAt)}
	}
	deduct, charge, err := h.legacyServices().Tx.SumsInPeriod(r.Context(), filter)
	if err != nil {
		http.Error(w, "거래 합계를 불러오지 못했습니다", 500)
		return
	}
	label := map[service.Period]string{service.PeriodToday: "오늘", service.PeriodWeek: "이번 주", service.PeriodMonth: "이번 달", service.PeriodAll: "전체"}[p]
	data := page.TransactionsData{Period: string(p), Type: typeFilter, Q: q, DeductLabel: label + " 차감", ChargeLabel: label + " 충전", DeductTotal: util.FormatKRW(deduct), ChargeTotal: util.FormatKRW(charge), Rows: views, Paging: paging}
	if isHTMX(r) && r.URL.Query().Get("more") == "1" {
		render(w, r, 200, page.TransactionsAppend(data))
		return
	}
	if isHTMX(r) && r.Header.Get("HX-Trigger-Name") == "q" {
		render(w, r, 200, page.TransactionsSearchResults(data))
		return
	}
	render(w, r, 200, page.Transactions(data))
}

func joinedDate(value string) string {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return t.In(kstLoc).Format("2006년 1월 2일")
}
