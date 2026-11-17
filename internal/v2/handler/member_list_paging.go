package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/v2/view/component"
)

const memberListPageSize = 20

func phoneListQuery(query string) string {
	if strings.IndexFunc(query, func(r rune) bool {
		return !(r >= '0' && r <= '9' || r == ' ' || r == '-' || r == '+' || r == '(' || r == ')')
	}) >= 0 {
		return ""
	}
	return util.NormalizePhone(query)
}

// listPaging slices one page, never a cumulative prefix. The optional start
// value describes pages already appended in this tab; normal GETs ignore it.
func listPaging(r *http.Request, total int, path, listID, footerID string, filters url.Values) (int, int, component.ListPagingProps) {
	pageNumber, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pages := (total + memberListPageSize - 1) / memberListPageSize
	if pages < 1 {
		pages = 1
	}
	if pageNumber < 1 {
		pageNumber = 1
	}
	if pageNumber > pages {
		pageNumber = pages
	}
	start := (pageNumber - 1) * memberListPageSize
	end := start + memberListPageSize
	if end > total {
		end = total
	}
	firstPage := pageNumber
	if isHTMX(r) && r.URL.Query().Get("more") == "1" {
		if value, err := strconv.Atoi(r.URL.Query().Get("start")); err == nil && value >= 1 && value <= pageNumber {
			firstPage = value
		}
	}
	from := (firstPage-1)*memberListPageSize + 1
	if total == 0 {
		from = 0
	}
	link := func(number int, more bool) string {
		q := url.Values{}
		for key, values := range filters {
			q[key] = append([]string(nil), values...)
		}
		q.Set("page", strconv.Itoa(number))
		if more {
			q.Set("more", "1")
			q.Set("start", strconv.Itoa(firstPage))
		}
		return path + "?" + q.Encode()
	}
	p := component.ListPagingProps{ID: footerID, TargetID: listID, Total: total, From: from, To: end, Page: pageNumber, Pages: pages}
	if pageNumber > 1 {
		p.PreviousURL = link(pageNumber-1, false)
	}
	if pageNumber < pages {
		p.NextURL, p.MoreURL = link(pageNumber+1, false), link(pageNumber+1, true)
	}
	return start, end, p
}
