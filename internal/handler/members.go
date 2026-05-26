package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mingicho/yuhada/internal/auth"
	"github.com/mingicho/yuhada/internal/db/dbgen"
	"github.com/mingicho/yuhada/internal/httpx"
	"github.com/mingicho/yuhada/internal/service"
	"github.com/mingicho/yuhada/internal/view/partial"
	"github.com/mingicho/yuhada/internal/sms"
	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/view/page"
)

// ─────────────────────────────────────────────
// Members list — GET /admin/members
// ─────────────────────────────────────────────
func (h *PageHandlers) MembersList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	members, err := h.Services.Member.Search(r.Context(), q, service.SortDefault)
	if err != nil {
		slog.Error("search members", "err", err)
		http.Error(w, err.Error(), 500)
		return
	}

	rows := make([]page.MemberRow, len(members))
	for i, m := range members {
		rows[i] = memberRowFromDB(m)
	}

	// HTMX 검색 디바운스 — partial만 반환 (hx-select가 #members-list 추출).
	if httpx.IsHTMX(r) && r.Header.Get("HX-Trigger-Name") == "q" {
		httpx.Render(w, r, http.StatusOK, page.MembersTable(rows))
		return
	}
	httpx.Render(w, r, http.StatusOK, page.Members(rows, q))
}

func memberRowFromDB(m dbgen.Member) page.MemberRow {
	row := page.MemberRow{
		ID:      m.ID,
		Name:    m.Name,
		Phone:   util.FormatPhone(m.Phone),
		Balance: util.FormatKRW(m.Balance),
		ZeroBal: m.Balance == 0,
		Lost:    !m.IsActive,
	}
	if m.CardUuid.Valid {
		row.Card = m.CardUuid.String
	}
	return row
}

// ─────────────────────────────────────────────
// New member — GET/POST /admin/members/new + /admin/members
// ─────────────────────────────────────────────
func (h *PageHandlers) NewMemberGet(w http.ResponseWriter, r *http.Request) {
	httpx.Render(w, r, http.StatusOK, page.NewMember(page.NewMemberProps{}))
}

func (h *PageHandlers) NewMemberPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	phone := r.FormValue("phone")
	card := strings.TrimSpace(r.FormValue("card"))
	memo := strings.TrimSpace(r.FormValue("memo"))
	amountStr := strings.ReplaceAll(r.FormValue("amount"), ",", "")

	rerender := func(msg string) {
		httpx.Render(w, r, http.StatusUnprocessableEntity, page.NewMember(page.NewMemberProps{
			Name:      name,
			Phone:     phone,
			Card:      card,
			AmountStr: amountStr,
			Memo:      memo,
			ErrorMsg:  msg,
		}))
	}

	if name == "" || phone == "" {
		rerender("이름과 전화번호는 필수입니다")
		return
	}

	created, err := h.Services.Member.Create(r.Context(), service.CreateMemberInput{
		Name:     name,
		Phone:    phone,
		CardUUID: card,
		Memo:     memo,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrDuplicatePhone):
			rerender("이미 등록된 전화번호입니다")
		case errors.Is(err, service.ErrDuplicateCard):
			rerender("이미 발급된 카드입니다")
		case errors.Is(err, service.ErrInvalidInput):
			rerender(err.Error())
		default:
			slog.Error("create member", "err", err)
			rerender("등록 중 오류가 발생했습니다")
		}
		return
	}

	// 초기 충전 (옵션) + 최초 충전 SMS
	if amountStr != "" {
		if amt, perr := strconv.ParseInt(amountStr, 10, 64); perr == nil && amt > 0 {
			adminID := auth.AdminIDFromContext(r.Context(), h.Session)
			if res, werr := h.Services.Wallet.Charge(r.Context(), created.ID, amt, "최초 충전", adminID); werr != nil {
				slog.Warn("initial charge failed", "err", werr, "member", created.ID)
			} else if h.SMS.Enabled() {
				go func() {
					msg := sms.FirstChargeMessage(created.Name, amt, res.NewBalance)
					if err := h.SMS.Send(created.Phone, msg); err != nil {
						slog.Error("sms send failed", "err", err, "member", created.ID)
					}
				}()
			}
		}
	}

	http.Redirect(w, r, "/admin/members/"+created.ID, http.StatusSeeOther)
}

// ─────────────────────────────────────────────
// Member detail — GET /admin/members/{id}
// ─────────────────────────────────────────────
func (h *PageHandlers) MemberDetailGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	m, err := h.Services.Member.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	txs, err := h.Services.Tx.ListByMember(r.Context(), id)
	if err != nil {
		slog.Error("list txs", "err", err)
		http.Error(w, err.Error(), 500)
		return
	}

	pm := buildMemberView(m, txs)
	httpx.Render(w, r, http.StatusOK, page.MemberDetail(pm))
}

func buildMemberView(m dbgen.Member, txs []dbgen.Transaction) page.Member {
	memo := ""
	if m.Memo.Valid {
		memo = m.Memo.String
	}
	card := ""
	if m.CardUuid.Valid {
		card = m.CardUuid.String
	}

	var totalIn, totalOut int64
	pTxs := make([]page.Tx, len(txs))
	for i, t := range txs {
		sign := "-"
		if t.Type == "charge" {
			sign = "+"
			totalIn += t.Amount
		} else if t.Type == "deduct" {
			totalOut += t.Amount
		}
		tmemo := ""
		if t.Memo.Valid {
			tmemo = t.Memo.String
		}
		pTxs[i] = page.Tx{
			Type:   t.Type,
			Memo:   tmemo,
			Amount: sign + util.FormatKRW(t.Amount),
			At:     formatTimeShort(t.CreatedAt),
		}
	}

	idShort := m.ID
	if len(idShort) > 8 {
		idShort = idShort[:8]
	}

	return page.Member{
		ID:         m.ID,
		IDFmt:      "#" + idShort,
		Name:       m.Name,
		Phone:      util.FormatPhone(m.Phone),
		JoinedAt:   formatJoinDate(m.CreatedAt),
		Memo:       memo,
		Active:     m.IsActive,
		Card:       card,
		Balance:    util.FormatKRW(m.Balance),
		BalanceInt: m.Balance,
		ZeroBal:    m.Balance == 0,
		TotalIn:    util.FormatKRW(totalIn),
		TotalOut:   util.FormatKRW(totalOut),
		Txs:        pTxs,
	}
}

// ─────────────────────────────────────────────
// Memo edit — GET /admin/members/{id}/memo + /memo/edit, PATCH /memo
// ─────────────────────────────────────────────
func (h *PageHandlers) MemoView(w http.ResponseWriter, r *http.Request) {
	m, ok := h.loadMemberPart(w, r)
	if !ok {
		return
	}
	httpx.Render(w, r, http.StatusOK, page.MemberMemoRow(m))
}

func (h *PageHandlers) MemoEdit(w http.ResponseWriter, r *http.Request) {
	m, ok := h.loadMemberPart(w, r)
	if !ok {
		return
	}
	httpx.Render(w, r, http.StatusOK, page.MemberMemoEdit(m))
}

func (h *PageHandlers) MemoUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	newMemo := strings.TrimSpace(r.FormValue("memo"))
	if err := h.Services.Member.UpdateMemo(r.Context(), id, newMemo); err != nil {
		slog.Error("update memo", "err", err)
		http.Error(w, err.Error(), 500)
		return
	}
	m, ok := h.loadMemberPart(w, r)
	if !ok {
		return
	}
	httpx.Render(w, r, http.StatusOK, page.MemberMemoRow(m))
}

// ─────────────────────────────────────────────
// Delete — DELETE /admin/members/{id}
// ─────────────────────────────────────────────
func (h *PageHandlers) MemberDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Services.Member.Delete(r.Context(), id); err != nil {
		slog.Error("delete member", "err", err)
		http.Error(w, err.Error(), 500)
		return
	}
	slog.Info("member deleted", "id", id)
	httpx.HXRedirect(w, "/admin/members")
}

func (h *PageHandlers) DeleteModal(w http.ResponseWriter, r *http.Request) {
	m, ok := h.loadMemberPart(w, r)
	if !ok {
		return
	}
	httpx.Render(w, r, http.StatusOK, partial.DeleteModal(m))
}

// loadMemberPart — partial 응답들이 공통으로 쓰는 회원 조회 헬퍼.
func (h *PageHandlers) loadMemberPart(w http.ResponseWriter, r *http.Request) (page.Member, bool) {
	id := chi.URLParam(r, "id")
	dbm, err := h.Services.Member.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			http.NotFound(w, r)
		} else {
			http.Error(w, err.Error(), 500)
		}
		return page.Member{}, false
	}
	return buildMemberView(dbm, nil), true
}
