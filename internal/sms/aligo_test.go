package sms

import (
	"strings"
	"testing"
)

func TestFirstChargeMessage(t *testing.T) {
	msg := FirstChargeMessage("김지영", 200000, 200000)
	checks := []string{
		"[유하다 헤어]",
		"김지영님, 반갑습니다",
		"₩200,000 충전",
		"잔액 ₩200,000",
		"정성껏 모시겠습니다",
		"1년(12개월)", // 약관
		"환불은 불가",  // 약관
	}
	for _, c := range checks {
		if !strings.Contains(msg, c) {
			t.Errorf("FirstChargeMessage should contain %q\ngot: %s", c, msg)
		}
	}
}

func TestChargeMessage(t *testing.T) {
	msg := ChargeMessage("박향신", 100000, 350000)
	checks := []string{
		"[유하다 헤어]",
		"박향신님",
		"₩100,000 충전",
		"잔액 ₩350,000",
		"환불은 불가", // 약관 포함
	}
	for _, c := range checks {
		if !strings.Contains(msg, c) {
			t.Errorf("ChargeMessage should contain %q\ngot: %s", c, msg)
		}
	}
	if strings.Contains(msg, "반갑습니다") {
		t.Error("ChargeMessage should NOT contain welcome greeting")
	}
}

func TestDeductMessage(t *testing.T) {
	msg := DeductMessage("황윤하", "디자이너컷", 50000, 150000)
	checks := []string{
		"[유하다 헤어]",
		"황윤하님",
		"디자이너컷 ₩50,000 차감",
		"잔액 ₩150,000",
		"다음 방문도 기다리겠습니다",
	}
	for _, c := range checks {
		if !strings.Contains(msg, c) {
			t.Errorf("DeductMessage should contain %q\ngot: %s", c, msg)
		}
	}
	if strings.Contains(msg, "환불") {
		t.Error("DeductMessage should NOT contain disclaimer")
	}
}

func TestFmtKRW(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "₩0"},
		{50000, "₩50,000"},
		{1234567, "₩1,234,567"},
		{-100000, "₩100,000"}, // fmtKRW takes absolute
	}
	for _, tt := range tests {
		got := fmtKRW(tt.in)
		if got != tt.want {
			t.Errorf("fmtKRW(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestClientEnabled(t *testing.T) {
	c := New("key", "user", "01012345678")
	if !c.Enabled() {
		t.Error("client with key should be enabled")
	}

	c2 := New("", "", "")
	if c2.Enabled() {
		t.Error("client without key should be disabled")
	}

	var c3 *Client
	if c3.Enabled() {
		t.Error("nil client should be disabled")
	}
}

func TestSendDisabledNoOp(t *testing.T) {
	c := New("", "", "")
	if err := c.Send("01012345678", "test"); err != nil {
		t.Errorf("disabled client should return nil, got %v", err)
	}
}
