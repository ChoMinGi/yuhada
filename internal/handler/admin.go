package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mingicho/yuhada/internal/auth"
	"github.com/mingicho/yuhada/internal/db/dbgen"
	"github.com/mingicho/yuhada/internal/httpx"
	"github.com/mingicho/yuhada/internal/service"
	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/view/page"
	"github.com/mingicho/yuhada/internal/view/partial"
)

// 한국어 요일.
var weekdayKR = map[time.Weekday]string{
	time.Sunday:    "일요일",
	time.Monday:    "월요일",
	time.Tuesday:   "화요일",
	time.Wednesday: "수요일",
	time.Thursday:  "목요일",
	time.Friday:    "금요일",
	time.Saturday:  "토요일",
}

func todayKorean(t time.Time) string {
	return fmt.Sprintf("%d년 %d월 %d일 %s", t.Year(), int(t.Month()), t.Day(), weekdayKR[t.Weekday()])
}

func formatTimeShort(iso string) string {
	t, err := time.Parse("2006-01-02T15:04:05.000Z", iso)
	if err != nil {
		t, err = time.Parse(time.RFC3339, iso)
		if err != nil {
			return iso
		}
	}
	t = t.Local()
	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		hour := t.Hour()
		ampm := "오전"
		if hour >= 12 {
			ampm = "오후"
			if hour > 12 {
				hour -= 12
			}
		}
		if hour == 0 {
			hour = 12
		}
		return fmt.Sprintf("%s %d:%02d", ampm, hour, t.Minute())
	}
	return fmt.Sprintf("%d월 %d일", int(t.Month()), t.Day())
}

func formatJoinDate(iso string) string {
	t, err := time.Parse("2006-01-02T15:04:05.000Z", iso)
	if err != nil {
		t, err = time.Parse(time.RFC3339, iso)
		if err != nil {
			return iso
		}
	}
	return fmt.Sprintf("%d년 %d월 %d일", t.Year(), int(t.Month()), t.Day())
}

// ─────────────────────────────────────────────
// Dashboard — GET /admin?period=today|week|month|all
// ─────────────────────────────────────────────
func (h *PageHandlers) AdminHome(w http.ResponseWriter, r *http.Request) {
	p := normalizePeriod(r.URL.Query().Get("period"))

	snap, err := h.Services.Stats.Dashboard(r.Context(), p)
	if err != nil {
		slog.Error("dashboard", "err", err)
		http.Error(w, err.Error(), 500)
		return
	}
	recent, err := h.Services.Tx.ListRecent(r.Context(), 8)
	if err != nil {
		slog.Error("recent tx", "err", err)
		http.Error(w, err.Error(), 500)
		return
	}

	activity := make([]page.Activity, len(recent))
	for i, t := range recent {
		sign := "-"
		if t.TxType == "charge" {
			sign = "+"
		}
		memo := ""
		if t.TxMemo.Valid {
			memo = t.TxMemo.String
		}
		activity[i] = page.Activity{
			Type:       t.TxType,
			MemberName: t.MemberName,
			Memo:       memo,
			Amount:     sign + util.FormatKRW(t.TxAmount),
			When:       formatTimeShort(t.TxCreatedAt),
		}
	}

	now := time.Now()
	httpx.Render(w, r, http.StatusOK, page.Dashboard(page.DashboardData{
		Period:            string(p),
		Eyebrow:           periodEyebrow(p),
		Headline:          periodHeadline(p, now),
		DeductLabel:       periodLabel(p, "차감"),
		ChargeLabel:       periodLabel(p, "충전"),
		ChargeTotal:       util.FormatKRW(snap.ChargeTotal),
		DeductTotal:       util.FormatKRW(snap.DeductTotal),
		ActiveMemberCount: snap.MemberCount,
		Activity:          activity,
	}))
}

// normalizePeriod — query string → 유효한 Period. 알 수 없으면 today.
func normalizePeriod(s string) service.Period {
	switch service.Period(s) {
	case service.PeriodWeek:
		return service.PeriodWeek
	case service.PeriodMonth:
		return service.PeriodMonth
	case service.PeriodAll:
		return service.PeriodAll
	default:
		return service.PeriodToday
	}
}

func periodEyebrow(p service.Period) string {
	switch p {
	case service.PeriodWeek:
		return "THIS WEEK"
	case service.PeriodMonth:
		return "THIS MONTH"
	case service.PeriodAll:
		return "ALL TIME"
	default:
		return "TODAY"
	}
}

func periodHeadline(p service.Period, now time.Time) string {
	switch p {
	case service.PeriodWeek:
		return "이번 주"
	case service.PeriodMonth:
		return fmt.Sprintf("%d년 %d월", now.Year(), int(now.Month()))
	case service.PeriodAll:
		return "전체 기간"
	default:
		return todayKorean(now)
	}
}

// periodLabel — "오늘 차감", "이번 주 충전" 등 KPI 카드 라벨.
func periodLabel(p service.Period, kind string) string {
	switch p {
	case service.PeriodWeek:
		return "이번 주 " + kind
	case service.PeriodMonth:
		return "이번 달 " + kind
	case service.PeriodAll:
		return "전체 " + kind
	default:
		return "오늘 " + kind
	}
}

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

	// 초기 충전 (옵션)
	if amountStr != "" {
		if amt, perr := strconv.ParseInt(amountStr, 10, 64); perr == nil && amt > 0 {
			adminID := auth.AdminIDFromContext(r.Context(), h.Session)
			if _, werr := h.Services.Wallet.Charge(r.Context(), created.ID, amt, "최초 충전", adminID); werr != nil {
				slog.Warn("initial charge failed", "err", werr, "member", created.ID)
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

// ─────────────────────────────────────────────
// Card issuance / lost / reactivate
// ─────────────────────────────────────────────
func (h *PageHandlers) CardIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	uuid := util.NewCardUUID()
	if err := h.Services.Member.IssueCard(r.Context(), id, uuid); err != nil {
		slog.Error("issue card", "err", err)
		http.Error(w, err.Error(), 500)
		return
	}
	m, ok := h.loadMemberPart(w, r)
	if !ok {
		return
	}
	httpx.Render(w, r, http.StatusOK, page.MemberCardRow(m))
}

func (h *PageHandlers) CardLost(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Services.Member.ReportLost(r.Context(), id); err != nil {
		slog.Error("report lost", "err", err)
		http.Error(w, err.Error(), 500)
		return
	}
	// 전체 페이지 재로드 (헤더 액션 영역도 바뀌므로 hx-target=body)
	w.Header().Set("HX-Redirect", "/admin/members/"+id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *PageHandlers) CardReactivate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Services.Member.Reactivate(r.Context(), id); err != nil {
		slog.Error("reactivate", "err", err)
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("HX-Redirect", "/admin/members/"+id)
	w.WriteHeader(http.StatusNoContent)
}

// ─────────────────────────────────────────────
// Modals (GET /admin/members/{id}/{kind}/modal)
// ─────────────────────────────────────────────
func (h *PageHandlers) ChargeModal(w http.ResponseWriter, r *http.Request) {
	m, ok := h.loadMemberPart(w, r)
	if !ok {
		return
	}
	httpx.Render(w, r, http.StatusOK, partial.ChargeModal(m))
}

func (h *PageHandlers) DeductModal(w http.ResponseWriter, r *http.Request) {
	m, ok := h.loadMemberPart(w, r)
	if !ok {
		return
	}
	httpx.Render(w, r, http.StatusOK, partial.DeductModal(m))
}

func (h *PageHandlers) LostModal(w http.ResponseWriter, r *http.Request) {
	m, ok := h.loadMemberPart(w, r)
	if !ok {
		return
	}
	httpx.Render(w, r, http.StatusOK, partial.LostModal(m))
}

func (h *PageHandlers) ReactivateModal(w http.ResponseWriter, r *http.Request) {
	m, ok := h.loadMemberPart(w, r)
	if !ok {
		return
	}
	httpx.Render(w, r, http.StatusOK, partial.ReactivateModal(m))
}

// ─────────────────────────────────────────────
// Charge / Deduct submit
// ─────────────────────────────────────────────
func (h *PageHandlers) ChargeSubmit(w http.ResponseWriter, r *http.Request) {
	h.walletSubmit(w, r, true)
}

func (h *PageHandlers) DeductSubmit(w http.ResponseWriter, r *http.Request) {
	h.walletSubmit(w, r, false)
}

func (h *PageHandlers) walletSubmit(w http.ResponseWriter, r *http.Request, isCharge bool) {
	id := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	// 입력 값에 천단위 콤마가 들어올 수 있어 strip 후 파싱.
	amtStr := strings.ReplaceAll(r.FormValue("amount"), ",", "")
	amt, err := strconv.ParseInt(amtStr, 10, 64)
	if err != nil || amt <= 0 {
		http.Error(w, "amount required", 400)
		return
	}
	memo := strings.TrimSpace(r.FormValue("memo"))
	adminID := auth.AdminIDFromContext(r.Context(), h.Session)

	var werr error
	if isCharge {
		_, werr = h.Services.Wallet.Charge(r.Context(), id, amt, memo, adminID)
	} else {
		_, werr = h.Services.Wallet.Deduct(r.Context(), id, amt, memo, adminID)
	}
	if werr != nil {
		slog.Error("wallet op", "err", werr, "id", id, "charge", isCharge)
		http.Error(w, werr.Error(), 400)
		return
	}

	// 성공: 잔액 카드 + 거래 내역 한 번에 갱신 + 모달 닫기 트리거.
	// 잔액 카드는 메인 swap, 거래 내역은 hx-swap-oob 로 자동 갱신.
	dbm, err := h.Services.Member.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	txs, _ := h.Services.Tx.ListByMember(r.Context(), id)
	m := buildMemberView(dbm, txs)

	w.Header().Set("HX-Trigger", "closemodal")
	httpx.Render(w, r, http.StatusOK, page.WalletResult(m))
}
