package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mingicho/yuhada/internal/service"
	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/view/page"

	"github.com/mingicho/yuhada/internal/httpx"
)

// ─────────────────────────────────────────────
// 시간 포맷 헬퍼 (dashboard·members 공용)
// ─────────────────────────────────────────────

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
