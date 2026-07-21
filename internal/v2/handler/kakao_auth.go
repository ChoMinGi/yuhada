package handler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"

	v2auth "github.com/mingicho/yuhada/internal/v2/auth"
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

	// 화이트리스트 미설정 시: 시도한 ID 를 화면 + 로그에 노출.
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
		slog.Warn("v2 kakao rejected (not whitelisted)", "kakao_id", u.ID)
		http.Error(w, "관리자 권한이 없는 카카오 계정입니다.", http.StatusForbidden)
		return
	}

	// 세션 갱신 (session fixation 방지 겸)
	_ = h.Deps.Session.RenewToken(r.Context())
	h.Deps.Session.Put(r.Context(), v2auth.SessionKeyRole, v2auth.RoleAdmin)
	h.Deps.Session.Put(r.Context(), "v2_kakao_id", fmt.Sprint(u.ID))
	slog.Info("v2 kakao login ok", "kakao_id", u.ID)

	next := q.Get("next")
	if next == "" || next[0] != '/' {
		next = "/v2/admin"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
