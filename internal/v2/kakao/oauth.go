// Package kakao — v2 관리자 카카오 OAuth 클라이언트.
//
// 격리 원칙: v1 kakao 코드 미공유. v2 만의 REDIRECT URI 로 별도 앱 사용 or
// 같은 앱에서 서로 다른 redirect_uri 등록.
package kakao

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	authorizeURL = "https://kauth.kakao.com/oauth/authorize"
	tokenURL     = "https://kauth.kakao.com/oauth/token"
	userInfoURL  = "https://kapi.kakao.com/v2/user/me"
)

// Config — 카카오 앱 자격 + redirect.
type Config struct {
	ClientID     string
	ClientSecret string // optional (앱 설정에 따라)
	RedirectURI  string
}

// Enabled — 최소 필수 필드 채워졌으면 true.
func (c Config) Enabled() bool {
	return c.ClientID != "" && c.RedirectURI != ""
}

// UserInfo — kapi /v2/user/me 응답 중 id 만 사용.
type UserInfo struct {
	ID int64 `json:"id"`
}

// AuthorizeURL — 카카오 로그인 페이지 URL. state 로 CSRF 방지.
func (c Config) AuthorizeURL(state string) string {
	v := url.Values{}
	v.Set("client_id", c.ClientID)
	v.Set("redirect_uri", c.RedirectURI)
	v.Set("response_type", "code")
	v.Set("state", state)
	return authorizeURL + "?" + v.Encode()
}

// ExchangeCode — authorization code → access_token.
func (c Config) ExchangeCode(ctx context.Context, code string) (string, error) {
	v := url.Values{}
	v.Set("grant_type", "authorization_code")
	v.Set("client_id", c.ClientID)
	v.Set("redirect_uri", c.RedirectURI)
	v.Set("code", code)
	if c.ClientSecret != "" {
		v.Set("client_secret", c.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(v.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("kakao token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("kakao token http %d: %s", resp.StatusCode, string(body))
	}
	var t struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &t); err != nil {
		return "", fmt.Errorf("kakao token parse: %w", err)
	}
	if t.AccessToken == "" {
		return "", fmt.Errorf("kakao token empty (body=%s)", string(body))
	}
	return t.AccessToken, nil
}

// FetchUser — access_token 으로 카카오 user_id 조회.
func (c Config) FetchUser(ctx context.Context, accessToken string) (UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", userInfoURL, nil)
	if err != nil {
		return UserInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return UserInfo{}, fmt.Errorf("kakao user request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return UserInfo{}, fmt.Errorf("kakao user http %d: %s", resp.StatusCode, string(body))
	}
	var u UserInfo
	if err := json.Unmarshal(body, &u); err != nil {
		return UserInfo{}, fmt.Errorf("kakao user parse: %w", err)
	}
	if u.ID == 0 {
		return UserInfo{}, fmt.Errorf("kakao user id missing (body=%s)", string(body))
	}
	return u, nil
}
