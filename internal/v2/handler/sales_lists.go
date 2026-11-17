package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/v2/view/page"
)

func (h *PageHandlers) SalesList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	values := r.URL.Query()
	kind := values.Get("kind")
	if kind != "top" && kind != "recharge" {
		kind = "recent"
	}
	period := normalizePeriod(values.Get("period"))
	if !isValidPeriod(period) {
		period = "this-month"
	}
	since, until, label := periodBounds(period)
	from, to := values.Get("from"), values.Get("to")
	if period == "custom" {
		start, end, customLabel, ok := parseCustomRange(from, to)
		if ok {
			since, until, label = toUTCIso(start), toUTCIso(end), customLabel
		} else {
			period = "this-month"
			since, until, label = periodBounds(period)
		}
	}
	query := strings.TrimSpace(values.Get("q"))
	number, _ := strconv.Atoi(values.Get("page"))
	data, err := h.Deps.Services.Sales.ListDetails(r.Context(), kind, since, until, query, number, time.Now())
	if err != nil {
		slog.Error("sales detail list", "err", err)
		http.Error(w, "판매 목록을 불러오지 못했습니다", 500)
		return
	}
	p := page.SalesListProps{Kind: kind, Period: period, From: from, To: to, Query: query, Title: map[string]string{"recent": "판매 이력", "top": "고객별 판매 금액", "recharge": "재충전 안내 후보"}[kind], Scope: label, Total: data.Total, Page: data.Page, Pages: data.Pages, Append: isHTMX(r) && values.Get("more") == "1"}
	_, _, p.Paging = listPaging(r, data.Total, "/v2/admin/sales/list", "sales-list-results", "sales-list-more", url.Values{"kind": {kind}, "period": {period}, "from": {from}, "to": {to}, "q": {query}})
	p.Paging.Unit = "건"
	if kind != "recent" {
		p.Paging.Unit = "명"
	}
	if kind == "recharge" {
		p.Scope = "현재 기준 · 잔액 5만원 이하 · 최근 60일 시술 고객"
	}
	if len(data.Rows) > 0 {
		p.Start = (data.Page-1)*20 + 1
		p.End = p.Start + len(data.Rows) - 1
	}
	for _, row := range data.Rows {
		detail := row.Detail
		date := formatKST(row.Date)
		if kind == "top" {
			detail = fmt.Sprintf("%d위 · 충전 %d건", row.Rank, row.Count)
			date = ""
		}
		if kind == "recharge" {
			detail = "현재 잔액 · 최근 시술"
		}
		p.Rows = append(p.Rows, page.SalesListRow{ID: row.ID, Href: "/v2/admin/members/" + url.PathEscape(row.MemberID), Name: row.Name, Detail: detail, Date: date, Amount: util.FormatKRW(row.Amount)})
	}
	link := func(number int) string {
		return "/v2/admin/sales/list?" + url.Values{"kind": {kind}, "period": {period}, "from": {from}, "to": {to}, "q": {query}, "page": {strconv.Itoa(number)}}.Encode()
	}
	if data.Page < data.Pages {
		p.NextURL = link(data.Page + 1)
	}
	if data.Page > 1 {
		p.PreviousURL = link(data.Page - 1)
	}
	if p.Append {
		render(w, r, 200, page.SalesListAppend(p))
		return
	}
	if isHTMX(r) {
		render(w, r, 200, page.SalesListResults(p))
		return
	}
	render(w, r, 200, page.SalesList(p))
}
