// Package httpx — HTTP 응답 유틸.
package httpx

import (
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
)

// Render — templ 컴포넌트를 HTTP 응답으로.
// HTMX 요청 여부는 핸들러에서 직접 분기 후 partial/page 선택.
func Render(w http.ResponseWriter, r *http.Request, status int, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := component.Render(r.Context(), w); err != nil {
		slog.Error("templ render", "err", err, "path", r.URL.Path)
	}
}

// IsHTMX — HTMX 요청 판별.
func IsHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// HXRedirect — HTMX 환경에서 full page redirect.
func HXRedirect(w http.ResponseWriter, url string) {
	w.Header().Set("HX-Redirect", url)
	w.WriteHeader(http.StatusNoContent)
}
