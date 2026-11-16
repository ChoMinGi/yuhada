package handler

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"

	"github.com/mingicho/yuhada/internal/auth"
)

// legacyAdminRedirect retires the old UI without replaying a stale wallet write.
func legacyAdminRedirect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, "/v2/", http.StatusTemporaryRedirect)
			return
		}
		if r.URL.Path != "/admin" && !strings.HasPrefix(r.URL.Path, "/admin/") {
			next.ServeHTTP(w, r)
			return
		}
		value := r.URL.RequestURI()
		// Both route trees accept the list/detail URLs without a trailing slash.
		if strings.HasSuffix(r.URL.Path, "/") {
			u := *r.URL
			u.Path = strings.TrimSuffix(u.Path, "/")
			u.RawPath = ""
			value = u.RequestURI()
		}
		target := auth.SafeAdminNext(value, "/admin")
		if r.Method != http.MethodGet && r.Method != http.MethodHead || legacyFragmentPath(r.URL.Path) {
			copyRequest := r.Clone(r.Context())
			copyRequest.Method = http.MethodPost
			target = auth.AdminReturnTo(copyRequest, "/admin")
		}
		if strings.ContainsAny(r.URL.Path, "\\\r\n") {
			target = "/admin"
		}
		if u, err := url.Parse(target); err == nil && (u.Path == "/admin" || strings.HasPrefix(u.Path, "/admin/")) {
			u.Path = "/v2" + u.Path
			u.RawPath = ""
			target = u.RequestURI()
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", target)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			http.Redirect(w, r, target, http.StatusTemporaryRedirect)
			return
		}
		// A 307/308 would resend a charge or deduction. Explicitly reject stale
		// submissions instead; even old fetch clients must not read login HTML as success.
		w.Header().Set("Location", target)
		const message = "관리자 화면이 새 주소로 이동했습니다. 이 요청은 처리되지 않았습니다. 새 화면에서 다시 진행해 주세요."
		if strings.Contains(r.Header.Get("Accept"), "application/json") || r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": message, "redirect": target})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_, _ = fmt.Fprintf(w, "<!doctype html><html lang=\"ko\"><meta charset=\"utf-8\"><title>새 관리자 화면</title><p>%s</p><p><a href=\"%s\">새 관리자 화면으로 이동</a></p></html>", message, html.EscapeString(target))
	})
}

func legacyFragmentPath(path string) bool {
	return strings.HasSuffix(path, "/modal") || strings.HasSuffix(path, "/memo") || strings.HasSuffix(path, "/memo/edit")
}
