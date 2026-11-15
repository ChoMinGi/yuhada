package handler

import (
	"net/http"
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
	return service.New(h.Deps.Services.DB, h.Deps.Services.Enc)
}

func (h *PageHandlers) CardsList(w http.ResponseWriter, r *http.Request) {
	all, err := h.legacyServices().Member.Search(r.Context(), "", service.SortDefault)
	if err != nil {
		http.Error(w, "카드 목록을 불러오지 못했습니다", 500)
		return
	}
	d := page.CardsData{}
	for _, m := range all {
		if m.CardUuid.Valid && m.CardUuid.String != "" {
			d.Issued++
			if m.IsActive {
				d.Active++
			} else {
				d.Lost++
			}
			d.Cards = append(d.Cards, page.CardRow{MemberID: m.ID, Name: m.Name, Card: m.CardUuid.String, IssuedAt: joinedDate(m.CreatedAt), Active: m.IsActive})
		} else if m.IsActive {
			d.Unissued = append(d.Unissued, page.MemberRow{ID: m.ID, Name: m.Name, Phone: util.FormatPhone(m.Phone), Balance: util.FormatKRW(m.Balance), ZeroBal: m.Balance == 0})
		}
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
	rows, err := h.legacyServices().Tx.ListInPeriod(r.Context(), filter)
	if err != nil {
		http.Error(w, "거래 내역을 불러오지 못했습니다", 500)
		return
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
		views[i] = page.TxRow{Type: row.TxType, MemberID: row.MemberID, MemberName: row.MemberName, Memo: memo, Amount: sign + util.FormatKRW(row.TxAmount), At: formatKST(row.TxCreatedAt)}
	}
	if isHTMX(r) && r.Header.Get("HX-Trigger-Name") == "q" {
		render(w, r, 200, page.TransactionsList(views))
		return
	}
	deduct, charge, err := h.legacyServices().Tx.SumsInPeriod(r.Context(), filter)
	if err != nil {
		http.Error(w, "거래 합계를 불러오지 못했습니다", 500)
		return
	}
	label := map[service.Period]string{service.PeriodToday: "오늘", service.PeriodWeek: "이번 주", service.PeriodMonth: "이번 달", service.PeriodAll: "전체"}[p]
	render(w, r, 200, page.Transactions(page.TransactionsData{Period: string(p), Type: typeFilter, Q: q, DeductLabel: label + " 차감", ChargeLabel: label + " 충전", DeductTotal: util.FormatKRW(deduct), ChargeTotal: util.FormatKRW(charge), Rows: views}))
}

func joinedDate(value string) string {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return t.In(kstLoc).Format("2006년 1월 2일")
}
