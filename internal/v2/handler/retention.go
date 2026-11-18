package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/mingicho/yuhada/internal/util"
	v2service "github.com/mingicho/yuhada/internal/v2/service"
	"github.com/mingicho/yuhada/internal/v2/view/page"
)

// RetentionDashboard reads the canonical service history. It never writes a
// wallet record, changes membership status, or sends a message.
func (h *PageHandlers) RetentionDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	opts := retentionOptions(r, time.Now())
	result, err := h.Deps.Services.Sales.Retention(r.Context(), opts)
	if err != nil {
		slog.Error("v2 retention dashboard", "err", err)
		http.Error(w, "고객 재방문 현황을 불러오지 못했습니다", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, page.Retention(buildRetentionProps(result)))
}

func retentionOptions(r *http.Request, now time.Time) v2service.RetentionOptions {
	months, _ := strconv.Atoi(r.URL.Query().Get("months"))
	if months != 3 && months != 6 && months != 12 && months != 24 {
		months = 3
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("absent"))
	if days != 60 && days != 90 && days != 120 {
		days = 60
	}
	pageNumber, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if pageNumber < 1 {
		pageNumber = 1
	}
	return v2service.RetentionOptions{Now: now, Months: months, DormantDays: days, Page: pageNumber}
}

func retentionURL(months, days, number int) string {
	return "/v2/admin/retention?" + url.Values{
		"months": {strconv.Itoa(months)}, "absent": {strconv.Itoa(days)}, "page": {strconv.Itoa(number)},
	}.Encode()
}

func retentionRate(numerator, denominator int) string {
	if denominator == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", float64(numerator)*100/float64(denominator))
}

func retentionGap(days float64, count int) string {
	if count == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f일", days)
}

func buildRetentionProps(data v2service.RetentionDashboard) page.RetentionProps {
	p := page.RetentionProps{
		Months: len(data.Months), DormantDays: data.DormantDays,
		AsOf:         data.AsOf.In(kstLoc).Format("2006.01.02 15:04"),
		HistoryStart: data.HistoryStart, LatestVisit: data.LatestVisit,
		MeanGap:       retentionGap(data.MeanGapDays, data.GapCount),
		GapDetail:     fmt.Sprintf("선택 기간에 끝난 방문 간격 %d개 기준", data.GapCount),
		FirstVisitors: strconv.Itoa(data.FirstVisitorCount) + "명",
		DormantCount:  data.DormantCount, NeverVisitedCount: data.NeverVisitedCount,
		Page: data.DormantPage, Pages: data.DormantPages,
		CurrentRate: "—", CurrentDetail: "이번 달 방문 기록 없음",
	}
	for _, row := range data.Months {
		width := "0"
		if row.Visitors > 0 {
			width = strconv.FormatFloat(float64(row.Returning)*100/float64(row.Visitors), 'f', 2, 64)
		}
		p.Rows = append(p.Rows, page.RetentionMonthRow{
			Month: row.Month, Visitors: strconv.Itoa(row.Visitors), Returning: strconv.Itoa(row.Returning),
			VisitDays: strconv.Itoa(row.VisitDays), Rate: retentionRate(row.Returning, row.Visitors),
			Gap: retentionGap(row.MeanGapDays, row.GapCount), RateWidth: width, Partial: row.Partial,
		})
		if row.Partial {
			p.CurrentRate = retentionRate(row.Returning, row.Visitors)
			p.CurrentDetail = fmt.Sprintf("이번 달 재방문 %d명 / 방문 고객 %d명 · 진행 중", row.Returning, row.Visitors)
		}
	}
	for _, member := range data.DormantMembers {
		p.DormantRows = append(p.DormantRows, page.RetentionDormantRow{
			Href: "/v2/admin/members/" + url.PathEscape(member.MemberID), Name: member.Name,
			LastVisit: member.LastVisit, Balance: util.FormatKRW(member.Balance), DaysAbsent: strconv.Itoa(member.DaysAbsent) + "일",
		})
	}
	for _, cohort := range data.Cohorts {
		row := page.RetentionCohortRow{Month: cohort.Month, Size: strconv.Itoa(cohort.Size) + "명"}
		for _, cell := range cohort.Cells {
			view := page.RetentionCohortCell{Class: cell.Status}
			switch cell.Status {
			case "complete":
				view.Label = retentionRate(cell.Returning, cohort.Size)
				view.Detail = fmt.Sprintf("%d / %d명", cell.Returning, cohort.Size)
				view.Class = retentionHeatClass(cell.Returning, cohort.Size)
			case "observing":
				view.Label, view.Detail = "관찰 중", "월 마감 전"
			default:
				view.Class, view.Label, view.Detail = "future", "—", "아직 도래하지 않음"
			}
			row.Cells = append(row.Cells, view)
		}
		p.Cohorts = append(p.Cohorts, row)
	}
	if p.Page > 1 {
		p.PreviousURL = retentionURL(p.Months, p.DormantDays, p.Page-1)
	}
	if p.Page < p.Pages {
		p.NextURL = retentionURL(p.Months, p.DormantDays, p.Page+1)
	}
	return p
}

func retentionHeatClass(numerator, denominator int) string {
	if denominator <= 0 || numerator <= 0 {
		return "zero"
	}
	rate := float64(numerator) / float64(denominator)
	switch {
	case rate >= 1:
		return "full"
	case rate >= .75:
		return "high"
	case rate >= .5:
		return "medium"
	default:
		return "low"
	}
}
