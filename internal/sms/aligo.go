// Package sms — 알리고 SMS API 클라이언트.
package sms

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const aligoEndpoint = "https://apis.aligo.in/send/"

// Client — 알리고 SMS 발송 클라이언트.
type Client struct {
	apiKey string
	userID string
	sender string
	http   *http.Client
}

// New — 클라이언트 생성. apiKey가 비어있으면 Enabled()=false.
func New(apiKey, userID, sender string) *Client {
	return &Client{
		apiKey: apiKey,
		userID: userID,
		sender: strings.ReplaceAll(sender, "-", ""),
		http:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Enabled — API 키가 설정되어 있으면 true.
func (c *Client) Enabled() bool {
	return c != nil && c.apiKey != ""
}

// aligoResponse — 알리고 API 응답.
type aligoResponse struct {
	ResultCode string `json:"result_code"`
	Message    string `json:"message"`
	MsgID      string `json:"msg_id"`
	SuccessCnt int    `json:"success_cnt"`
	ErrorCnt   int    `json:"error_cnt"`
}

// Send — LMS 1건 발송.
func (c *Client) Send(phone, msg string) error {
	if !c.Enabled() {
		return nil
	}

	phone = strings.ReplaceAll(phone, "-", "")

	data := url.Values{
		"key":      {c.apiKey},
		"user_id":  {c.userID},
		"sender":   {c.sender},
		"receiver": {phone},
		"msg":      {msg},
		"msg_type": {"LMS"},
		"title":    {"유하다 헤어"},
	}

	resp, err := c.http.PostForm(aligoEndpoint, data)
	if err != nil {
		return fmt.Errorf("aligo request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("aligo read body: %w", err)
	}

	var result aligoResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("aligo parse: %w (body=%s)", err, string(body))
	}

	if result.ResultCode != "1" {
		return fmt.Errorf("aligo error: code=%s msg=%s", result.ResultCode, result.Message)
	}

	slog.Info("sms sent", "to", phone, "msg_id", result.MsgID)
	return nil
}

// ─────────────────────────────────────────────
// 메시지 템플릿
// ─────────────────────────────────────────────

const chargeDisclaimer = "\n\n본 정액권은 모든 시술 및 제품 구매가 가능하며 사용 기간은 지급일로부터 1년(12개월)입니다. 가족 및 지인과 함께 사용 가능하며 부득이한 경우 양도는 가능하나 환불은 불가한 점을 양해 부탁드립니다. 감사합니다:)"

// FirstChargeMessage — 최초 충전(가입) 알림 + 이용약관.
func FirstChargeMessage(name string, amount, balance int64) string {
	return fmt.Sprintf("[유하다 헤어] %s님, 반갑습니다.\n%s 충전이 완료되었습니다.\n잔액 %s\n\n늘 정성껏 모시겠습니다.%s",
		name, fmtKRW(amount), fmtKRW(balance), chargeDisclaimer)
}

// DeductMessage — 차감 알림.
func DeductMessage(name, serviceName string, amount, balance int64) string {
	return fmt.Sprintf("[유하다 헤어] %s님, 오늘 방문해 주셔서 감사합니다.\n%s %s 차감\n잔액 %s\n\n다음 방문도 기다리겠습니다.",
		name, serviceName, fmtKRW(amount), fmtKRW(balance))
}

// ChargeMessage — 재충전 알림 + 이용약관.
func ChargeMessage(name string, amount, balance int64) string {
	return fmt.Sprintf("[유하다 헤어] %s님, %s 충전이 완료되었습니다.\n잔액 %s%s",
		name, fmtKRW(amount), fmtKRW(balance), chargeDisclaimer)
}

// fmtKRW — 123456 → "₩123,456"
func fmtKRW(v int64) string {
	if v < 0 {
		v = -v
	}
	s := fmt.Sprintf("%d", v)
	var out strings.Builder
	out.WriteString("₩")
	n := len(s)
	for i, c := range s {
		out.WriteRune(c)
		rem := n - i - 1
		if rem > 0 && rem%3 == 0 {
			out.WriteRune(',')
		}
	}
	return out.String()
}
