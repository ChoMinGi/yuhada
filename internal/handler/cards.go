package handler

import (
	"log/slog"
	"net/http"

	"github.com/mingicho/yuhada/internal/db/dbgen"
	"github.com/mingicho/yuhada/internal/httpx"
	"github.com/mingicho/yuhada/internal/service"
	"github.com/mingicho/yuhada/internal/util"
	"github.com/mingicho/yuhada/internal/view/page"
)

// GET /admin/cards
func (h *PageHandlers) CardsList(w http.ResponseWriter, r *http.Request) {
	all, err := h.Services.Member.Search(r.Context(), "", service.SortDefault)
	if err != nil {
		slog.Error("cards list", "err", err)
		http.Error(w, err.Error(), 500)
		return
	}

	d := page.CardsData{}
	var cards []page.CardRow
	var unissued []page.MemberRow
	for _, m := range all {
		if m.CardUuid.Valid && m.CardUuid.String != "" {
			d.Issued++
			if m.IsActive {
				d.Active++
			} else {
				d.Lost++
			}
			cards = append(cards, page.CardRow{
				MemberID: m.ID,
				Name:     m.Name,
				Card:     m.CardUuid.String,
				IssuedAt: formatJoinDate(m.CreatedAt),
				Active:   m.IsActive,
			})
		} else if m.IsActive {
			unissued = append(unissued, memberRowFromDB(m))
		}
	}
	d.Cards = cards
	d.Unissued = unissued
	httpx.Render(w, r, http.StatusOK, page.Cards(d))
}

// 사용 안 함 — `_ = dbgen.Member{}` 타입 보존을 위해 남겨둠.
var _ = dbgen.Member{}
var _ = util.FormatKRW
