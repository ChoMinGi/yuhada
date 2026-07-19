// Package sms — v2 SMS는 NoOp. 실제 발송 코드 없음.
//
// 격리 원칙(isolation-rules.md §1-3):
//   - PostForm/aligo/apis.aligo.in/http.Client 어느 것도 등장하지 않음.
//   - v1 internal/sms 를 import 하지 않음.
//   - Send 는 slog 로만 남기고 nil 반환.
package sms

import (
	"log/slog"
)

// Client — v1 sms.Client 와 시그니처 호환 (Enabled/Send 두 메서드).
type Client struct{}

// New — NoOp 클라이언트 생성.
func New() *Client { return &Client{} }

// Enabled — 항상 false. 핸들러는 이 값을 보고 실제 발송 코드를 스킵.
func (c *Client) Enabled() bool { return false }

// Send — slog 로만 기록. 실제 발송 안 함.
func (c *Client) Send(phone, msg string) error {
	slog.Info("v2 sms (noop)", "phone_last4", tail4(phone), "msg_preview", preview(msg))
	return nil
}

func preview(s string) string {
	const max = 40
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}

func tail4(s string) string {
	if len(s) <= 4 {
		return s
	}
	return s[len(s)-4:]
}
