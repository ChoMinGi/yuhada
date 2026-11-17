package handler

import (
	"net/http"
	"net/url"
	"testing"

	legacyauth "github.com/mingicho/yuhada/internal/auth"
)

func TestExpiredModalSignInReturnsToMemberPage(t *testing.T) {
	for _, suffix := range []string{"charge/modal", "deduct/modal", "lost/modal", "reactivate/modal", "delete/modal", "memo", "memo/edit"} {
		t.Run(suffix, func(t *testing.T) {
			f := newPinlessFixture(t, "owner@example.com", false)
			fragment := "/v2/admin/members/fixture-member/" + suffix
			req, _ := http.NewRequest("GET", "https://salon.example"+fragment, nil)
			req.Header.Set("HX-Request", "true")
			response, err := f.client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			login, err := url.Parse(response.Header.Get("HX-Redirect"))
			if response.StatusCode != 204 || err != nil || login.Query().Get("next") != "/v2/admin/members/fixture-member" {
				t.Fatalf("expired fragment returned %d %q %v", response.StatusCode, response.Header.Get("HX-Redirect"), err)
			}
			if code, location := f.signIn(fragment); code != 303 || location != "/v2/admin/members/fixture-member" {
				t.Fatalf("Kakao returned fragment %d %q", code, location)
			}
		})
	}
	full := "/v2/admin/transactions?period=all&type=charge&q=fixture"
	if got := legacyauth.SafeAdminNext(full, "/v2/admin"); got != full {
		t.Fatalf("full-page filter lost %q", got)
	}
	if got := legacyauth.SafeAdminNext("//outside.example/v2/admin/members/id/charge/modal", "/v2/admin"); got != "/v2/admin" {
		t.Fatalf("unsafe redirect %q", got)
	}
}
