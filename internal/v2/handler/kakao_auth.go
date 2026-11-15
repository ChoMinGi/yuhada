package handler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	v2auth "github.com/mingicho/yuhada/internal/v2/auth"
	v2kakao "github.com/mingicho/yuhada/internal/v2/kakao"
)

const kakaoStateSessionKey = "v2_kakao_state"

// KakaoLogin — GET /v2/kakao/login
// CSRF state 를 세션에 저장하고 카카오 authorize URL 로 redirect.
func (h *PageHandlers) KakaoLogin(w http.ResponseWriter, r *http.Request) {
	if h.Deps.Kakao == nil || !h.Deps.Kakao.Enabled() {
		http.Error(w, "카카오 로그인이 서버에 설정되지 않았습니다.", http.StatusServiceUnavailable)
		return
	}
	state := randHex(16)
	h.Deps.Session.Put(r.Context(), kakaoStateSessionKey, state)
	if next := r.URL.Query().Get("next"); next != "" {
		h.Deps.Session.Put(r.Context(), "v2_kakao_next", v2auth.SafeNext(next))
	}
	http.Redirect(w, r, h.Deps.Kakao.AuthorizeURL(state), http.StatusSeeOther)
}

// KakaoCallback — GET /v2/kakao/callback?code=&state=
// 1) state 검증 (CSRF) 2) code → token 3) token → user_id 4) 화이트리스트 체크 5) 세션 부여.
func (h *PageHandlers) KakaoCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	code := q.Get("code")
	state := q.Get("state")

	expected, _ := h.Deps.Session.Get(r.Context(), kakaoStateSessionKey).(string)
	h.Deps.Session.Remove(r.Context(), kakaoStateSessionKey)
	if state == "" || state != expected {
		http.Error(w, "state mismatch (CSRF 방지)", http.StatusBadRequest)
		return
	}
	if code == "" {
		http.Error(w, "code 누락", http.StatusBadRequest)
		return
	}

	tok, err := h.Deps.Kakao.ExchangeCode(r.Context(), code)
	if err != nil {
		slog.Error("v2 kakao token exchange", "err", err)
		http.Error(w, "카카오 토큰 교환 실패", http.StatusBadGateway)
		return
	}
	u, err := h.Deps.Kakao.FetchUser(r.Context(), tok)
	if err != nil {
		slog.Error("v2 kakao user fetch", "err", err)
		http.Error(w, "카카오 사용자 조회 실패", http.StatusBadGateway)
		return
	}

	// 이메일 설정이 있으면 반드시 검증된 이메일로 판단한다. ID fallback은 금지.
	if strings.TrimSpace(h.Deps.AdminKakaoEmails) != "" {
		if !verifiedKakaoEmail(u.KakaoAccount) {
			http.Error(w, "카카오 계정의 이메일 제공 동의와 유효한 이메일 인증이 필요합니다.", http.StatusForbidden)
			return
		}
		if !kakaoEmailAllowed(u.KakaoAccount.Email, h.Deps.AdminKakaoEmails) {
			http.Error(w, "관리자로 등록되지 않은 이메일입니다. 관리자에게 문의해 주세요.", http.StatusForbidden)
			return
		}
	} else {
		// 화이트리스트 미설정 시: 기존 ID 등록 안내 유지.
		// 사용자가 이 값을 ADMIN_KAKAO_ID_V2 env 에 넣고 재시작하면 로그인 활성.
		if h.Deps.AdminKakaoID == "" {
			slog.Info("v2 kakao login attempt (whitelist empty)", "kakao_id", u.ID)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>관리자 미설정</title>
<div style="font:14px/1.6 system-ui;padding:48px 24px;max-width:520px;margin:0 auto;color:#1A1614">
  <h1 style="font:italic 22px serif;margin:0 0 12px">관리자 등록이 필요합니다</h1>
  <p>카카오 인증은 성공했지만 아직 관리자로 등록된 카카오 ID 가 없습니다.</p>
  <p>서버 관리자에게 아래 값을 전달해 주세요:</p>
  <pre style="background:#F0E4D0;padding:12px 16px;border-radius:6px;font:14px monospace">ADMIN_KAKAO_ID_V2=%d</pre>
</div>`, u.ID)
			return
		}
		if fmt.Sprint(u.ID) != h.Deps.AdminKakaoID {
			slog.Warn("v2 kakao rejected (not whitelisted)")
			http.Error(w, "관리자 권한이 없는 카카오 계정입니다.", http.StatusForbidden)
			return
		}
	}

	// 세션 갱신 (session fixation 방지 겸)
	if err := h.Deps.Session.RenewToken(r.Context()); err != nil {
		slog.Error("v2 kakao session renewal failed")
		http.Error(w, "로그인 세션을 생성하지 못했습니다. 다시 시도해 주세요.", http.StatusInternalServerError)
		return
	}
	h.Deps.Session.Put(r.Context(), v2auth.SessionKeyRole, v2auth.RoleAdmin)
	h.Deps.Session.Put(r.Context(), "v2_kakao_id", fmt.Sprint(u.ID))
	slog.Info("v2 kakao login ok")

	next := q.Get("next")
	if saved := h.Deps.Session.GetString(r.Context(), "v2_kakao_next"); saved != "" {
		next = saved
	}
	h.Deps.Session.Remove(r.Context(), "v2_kakao_next")
	if !safeKakaoNext(next) {
		next = "/v2/admin"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func verifiedKakaoEmail(account v2kakao.AccountInfo) bool {
	return strings.TrimSpace(account.Email) != "" &&
		account.EmailNeedsAgreement != nil && !*account.EmailNeedsAgreement &&
		account.IsEmailValid != nil && *account.IsEmailValid &&
		account.IsEmailVerified != nil && *account.IsEmailVerified
}

func kakaoEmailAllowed(email, whitelist string) bool {
	email = strings.TrimSpace(email)
	if email == "" {
		return false
	}
	for _, allowed := range strings.Split(whitelist, ",") {
		if allowed = strings.TrimSpace(allowed); allowed != "" && strings.EqualFold(email, allowed) {
			return true
		}
	}
	return false
}

func safeKakaoNext(next string) bool {
	parsed, err := url.Parse(next)
	return err == nil && strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") &&
		!strings.ContainsAny(next, "\\\r\n") && parsed.Host == "" && parsed.Scheme == ""
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
