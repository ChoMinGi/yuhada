package page

import (
	"math"
	"strconv"
	"strings"
)

// RetentionProps separates historical measurements from the current action list.
// All strings are display values; Class is a semantic heatmap tone, not CSS.
type RetentionProps struct {
	Months, DormantDays                            int
	AsOf, HistoryStart, LatestVisit                string
	Rows                                           []RetentionMonthRow
	CurrentRate, CurrentDetail, MeanGap, GapDetail string
	FirstVisitors                                  string
	DormantCount, NeverVisitedCount                int
	DormantRows                                    []RetentionDormantRow
	Cohorts                                        []RetentionCohortRow
	Page, Pages                                    int
	PreviousURL, NextURL                           string
}

type RetentionMonthRow struct {
	Month, Visitors, Returning, VisitDays, Rate, Gap, RateWidth string
	Partial                                                     bool
}

type RetentionDormantRow struct {
	Href, Name, LastVisit, Balance, DaysAbsent string
}

type RetentionCohortRow struct {
	Month, Size string
	Cells       []RetentionCohortCell
}

type RetentionCohortCell struct {
	Label, Detail, Class string
}

func retentionCohortPartial(p RetentionProps, row RetentionCohortRow) bool {
	if len(p.Rows) == 0 {
		return false
	}
	last := p.Rows[len(p.Rows)-1]
	return last.Partial && last.Month == row.Month
}

func retentionBarWidth(raw string) string {
	v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(raw), "%"), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		v = 0
	}
	v = math.Max(0, math.Min(100, v))
	return "width:" + strconv.FormatFloat(v, 'f', 1, 64) + "%"
}

func retentionCellClass(tone string) string {
	switch tone {
	case "full":
		return "bg-ink-900 text-sand-50"
	case "high":
		return "bg-ink-700 text-sand-50"
	case "medium":
		return "bg-sand-300 text-ink-900"
	case "low":
		return "bg-sand-200 text-ink-900"
	case "observing":
		return "bg-sand-100 text-ink-500"
	case "future":
		return "bg-sand-50 text-ink-500"
	default:
		return "bg-white text-ink-700"
	}
}
