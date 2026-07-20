package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/v2/db/dbgen"
	v2service "github.com/mingicho/yuhada/internal/v2/service"
	"github.com/mingicho/yuhada/internal/v2/view/page"
)

// MembersList — GET /v2/admin/members.
func (h *PageHandlers) MembersList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	members, err := h.Deps.Services.Member.Search(r.Context(), q, v2service.SortDefault)
	if err != nil {
		slog.Error("v2 members search", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rows := make([]page.MemberRow, len(members))
	for i, m := range members {
		rows[i] = memberRowFromDB(m)
	}

	// HTMX 검색 디바운스 partial.
	if isHTMX(r) && r.Header.Get("HX-Trigger-Name") == "q" {
		render(w, r, http.StatusOK, page.MembersTable(rows))
		return
	}
	render(w, r, http.StatusOK, page.Members(page.MembersProps{Rows: rows, Query: q}))
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

	rerender := func(msg string) {
		render(w, r, http.StatusUnprocessableEntity, page.NewMember(page.NewMemberProps{
			Name: name, Phone: phone, Card: card, Memo: memo, ErrorMsg: msg,
		}))
	}

	if name == "" || phone == "" {
		rerender("이름과 전화번호는 필수입니다")
		return
	}

	created, err := h.Deps.Services.Member.Create(r.Context(), v2service.CreateMemberInput{
		Name: name, Phone: phone, CardUUID: card, Memo: memo,
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
