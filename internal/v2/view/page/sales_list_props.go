package page

import "net/url"
import "github.com/mingicho/yuhada/internal/v2/view/component"

func salesListLink(p SalesProps, kind string) string {
	return "/v2/admin/sales/list?" + url.Values{"kind": {kind}, "period": {p.Period}, "from": {p.PeriodFrom}, "to": {p.PeriodTo}}.Encode()
}

type SalesListProps struct {
	Paging                                                            component.ListPagingProps
	Kind, Period, From, To, Query, Title, Scope, NextURL, PreviousURL string
	Rows                                                              []SalesListRow
	Total, Page, Pages, Start, End                                    int
	Append                                                            bool
}
type SalesListRow struct{ ID, Href, Name, Detail, Date, Amount string }
