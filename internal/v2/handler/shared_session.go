package handler

import (
	"context"
	"fmt"
	"net/http"

	legacyauth "github.com/mingicho/yuhada/internal/auth"
	v2auth "github.com/mingicho/yuhada/internal/v2/auth"
)

// authorizeLegacy mirrors only a verified Kakao session, never a PIN-only role.
// The managers retain distinct cookies, stores and context keys.
func (h *PageHandlers) authorizeLegacy(ctx context.Context, kakaoID string, forceRotate bool) error {
	sm := h.Deps.LegacySession
	if sm == nil {
		return nil
	}
	if kakaoID == "" {
		return fmt.Errorf("missing Kakao principal")
	}
	principal := "kakao:" + kakaoID
	if !forceRotate && legacyauth.IsAdmin(ctx, sm) && sm.GetString(ctx, legacyauth.SessionKeyAdminID) == principal {
		return nil
	}
	if err := sm.RenewToken(ctx); err != nil {
		return err
	}
	sm.Put(ctx, legacyauth.SessionKeyRole, legacyauth.RoleAdmin)
	sm.Put(ctx, legacyauth.SessionKeyAuthMethod, legacyauth.AuthMethodKakao)
	sm.Put(ctx, legacyauth.SessionKeyAdminID, principal)
	sm.Remove(ctx, legacyauth.SessionKeyEmail)
	return nil
}

func (h *PageHandlers) syncLegacySession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v2auth.IsAdmin(r.Context(), h.Deps.Session) {
			id := h.Deps.Session.GetString(r.Context(), v2auth.SessionKeyKakaoID)
			if err := h.authorizeLegacy(r.Context(), id, false); err != nil {
				http.Error(w, "로그인 세션을 생성하지 못했습니다. 다시 시도해 주세요.", http.StatusInternalServerError)
				return
			}
			if principal := "kakao:" + id; h.Deps.Session.GetString(r.Context(), v2auth.SessionKeyAdminID) != principal {
				h.Deps.Session.Put(r.Context(), v2auth.SessionKeyAdminID, principal)
			}
		}
		next.ServeHTTP(w, r)
	})
}
