package kakao

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAuthorizeURLEmailScope(t *testing.T) {
	for _, require := range []bool{false, true} {
		cfg := Config{ClientID: "example-client", RedirectURI: "https://salon.example/v2/kakao/callback", RequireEmail: require}
		u, err := url.Parse(cfg.AuthorizeURL("state with + symbols"))
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		if u.Host != "kauth.kakao.com" || q.Get("state") != "state with + symbols" || q.Get("client_id") != cfg.ClientID || q.Get("redirect_uri") != cfg.RedirectURI || q.Get("response_type") != "code" {
			t.Fatalf("bad authorize URL: %s", u)
		}
		if require && q.Get("scope") != "account_email" {
			t.Fatal("email scope missing")
		}
		if !require && q.Has("scope") {
			t.Fatal("legacy login requested scope")
		}
	}
}

func TestFetchUserEmailJSON(t *testing.T) {
	for _, tc := range []struct {
		name, body         string
		wantErr, wantEmail bool
	}{
		{"verified", `{"id":12,"kakao_account":{"email":"owner@example.com","email_needs_agreement":false,"is_email_valid":true,"is_email_verified":true}}`, false, true},
		{"legacy", `{"id":12}`, false, false},
		{"missing id", `{"kakao_account":{"email":"private@example.com"}}`, true, false},
		{"invalid JSON", `{"id":`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Fatal("missing bearer token")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}}
			u, err := cfg.FetchUser(context.Background(), "test-token")
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
			if tc.wantEmail && (u.KakaoAccount.Email != "owner@example.com" || u.KakaoAccount.EmailNeedsAgreement == nil || *u.KakaoAccount.EmailNeedsAgreement || u.KakaoAccount.IsEmailValid == nil || !*u.KakaoAccount.IsEmailValid || u.KakaoAccount.IsEmailVerified == nil || !*u.KakaoAccount.IsEmailVerified) {
				t.Fatalf("email flags not parsed: %+v", u.KakaoAccount)
			}
			if !tc.wantErr && !tc.wantEmail && u.KakaoAccount.IsEmailVerified != nil {
				t.Fatal("missing flag must stay nil")
			}
		})
	}
}

func TestOAuthErrorsDoNotExposeResponse(t *testing.T) {
	const secret = "private-response-secret@example.com"
	for _, status := range []int{200, 400} {
		cfg := Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"private":"` + secret + `"}`))}, nil
		})}}
		_, tokenErr := cfg.ExchangeCode(context.Background(), "code")
		_, userErr := cfg.FetchUser(context.Background(), "token")
		for _, err := range []error{tokenErr, userErr} {
			if err == nil || strings.Contains(err.Error(), secret) {
				t.Fatalf("unsafe/missing error: %v", err)
			}
		}
	}
}
