package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mingicho/yuhada/internal/sms"
	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/v2/db/dbgen"
	v2service "github.com/mingicho/yuhada/internal/v2/service"
	"github.com/mingicho/yuhada/internal/v2/view/page"
)

// MembersList — GET /v2/admin/members.
func (h *PageHandlers) MembersList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	status := r.URL.Query().Get("status")
	if status != "active" && status != "inactive" {
		status = "all"
	}
	sortKey := v2service.SortKey(r.URL.Query().Get("sort"))
	if sortKey != v2service.SortBalanceAsc && sortKey != v2service.SortBalanceDesc {
		sortKey = v2service.SortDefault
	}
	searchQuery := q
	if phone := phoneListQuery(q); phone != "" {
		searchQuery = phone
	}
	members, err := h.Deps.Services.Member.Search(r.Context(), searchQuery, sortKey)
	if err != nil {
		slog.Error("v2 members search", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	filtered := members[:0]
	for _, m := range members {
		if status == "active" && !m.IsActive || status == "inactive" && m.IsActive {
			continue
		}
		filtered = append(filtered, m)
	}
	start, end, paging := listPaging(r, len(filtered), "/v2/admin/members", "members-list", "members-paging", url.Values{"q": {q}, "status": {status}, "sort": {string(sortKey)}})
	rows := make([]page.MemberRow, 0, end-start)
	for _, m := range filtered[start:end] {
		rows = append(rows, memberRowFromDB(m))
	}
	data := page.MembersProps{Rows: rows, Query: q, Status: status, Sort: string(sortKey), Paging: paging}
	if isHTMX(r) && r.URL.Query().Get("more") == "1" {
		render(w, r, http.StatusOK, page.MembersAppend(data))
		return
	}
	if isHTMX(r) && r.URL.Query().Get("filter") == "1" {
		render(w, r, http.StatusOK, page.MembersResults(data))
		return
	}
	render(w, r, http.StatusOK, page.Members(data))
}

// NewMemberGet — GET /v2/admin/members/new.
func (h *PageHandlers) NewMemberGet(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, page.NewMember(page.NewMemberProps{}))
}

// NewMemberPost — POST /v2/admin/members.
func (h *PageHandlers) NewMemberPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	phone := r.FormValue("phone")
	card := strings.TrimSpace(r.FormValue("card"))
	memo := strings.TrimSpace(r.FormValue("memo"))
	amountStr := strings.ReplaceAll(strings.TrimSpace(r.FormValue("amount")), ",", "")

	rerender := func(msg string) {
		render(w, r, http.StatusUnprocessableEntity, page.NewMember(page.NewMemberProps{
			Name: name, Phone: phone, Card: card, Memo: memo, AmountStr: amountStr, ErrorMsg: msg,
		}))
	}

	if name == "" || phone == "" {
		rerender("이름과 전화번호는 필수입니다")
		return
	}
	var initialCharge int64
	if amountStr != "" {
		var err error
		initialCharge, err = strconv.ParseInt(amountStr, 10, 64)
		if err != nil || initialCharge < 0 {
			rerender("초기 충전 금액을 확인해 주세요")
			return
		}
	}

	created, err := h.Deps.Services.Member.Create(r.Context(), v2service.CreateMemberInput{
		Name: name, Phone: phone, CardUUID: card, Memo: memo, InitialCharge: initialCharge, CreatedBy: h.adminID(r),
	})
	if err != nil {
		switch {
		case errors.Is(err, v2service.ErrDuplicatePhone):
			rerender("이미 등록된 전화번호입니다")
		case errors.Is(err, v2service.ErrDuplicateCard):
			rerender("이미 발급된 카드입니다")
		case errors.Is(err, v2service.ErrInvalidInput):
			rerender(err.Error())
		default:
			slog.Error("v2 create member", "err", err)
			rerender("등록 중 오류가 발생했습니다")
		}
		return
	}
	if initialCharge > 0 && h.Deps.SMS != nil && h.Deps.SMS.Enabled() {
		go func() {
			if err := h.Deps.SMS.Send(created.Phone, sms.FirstChargeMessage(created.Name, initialCharge, created.Balance)); err != nil {
				slog.Error("sms send failed", "member", created.ID)
			}
		}()
	}
	http.Redirect(w, r, "/v2/admin/members/"+created.ID, http.StatusSeeOther)
}

// MemberDelete — DELETE /v2/admin/members/{id}.
func (h *PageHandlers) MemberDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Deps.Services.Member.Delete(r.Context(), id); err != nil {
		slog.Error("v2 delete member", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	hxRedirect(w, "/v2/admin/members")
}

// ─────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────

func memberRowFromDB(m dbgen.V2Member) page.MemberRow {
	row := page.MemberRow{
		ID:       m.ID,
		Name:     m.Name,
		Phone:    util.FormatPhone(m.Phone),
		Balance:  util.FormatKRW(m.Balance),
		ZeroBal:  m.Balance == 0,
		Inactive: !m.IsActive,
	}
	if m.CardUuid.Valid {
		row.Card = m.CardUuid.String
	}
	return row
}
