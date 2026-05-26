package util

import "testing"

func TestFormatKRW(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "₩0"},
		{100, "₩100"},
		{1000, "₩1,000"},
		{50000, "₩50,000"},
		{123456, "₩123,456"},
		{1234567, "₩1,234,567"},
		{-50000, "₩-50,000"},
	}
	for _, tt := range tests {
		got := FormatKRW(tt.in)
		if got != tt.want {
			t.Errorf("FormatKRW(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizePhone(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"01012345678", "01012345678"},
		{"010-1234-5678", "01012345678"},
		{"+82 10-1234-5678", "01012345678"},
		{"82 10 1234 5678", "01012345678"},
		{"010.1234.5678", "01012345678"},
		{"", ""},
	}
	for _, tt := range tests {
		got := NormalizePhone(tt.in)
		if got != tt.want {
			t.Errorf("NormalizePhone(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatPhone(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"01012345678", "010-1234-5678"},
		{"0311234567", "031-123-4567"},
		{"010-1234-5678", "010-1234-5678"}, // already formatted → normalize → format
		{"123", "123"},                     // too short, return as-is
	}
	for _, tt := range tests {
		got := FormatPhone(tt.in)
		if got != tt.want {
			t.Errorf("FormatPhone(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNowISO(t *testing.T) {
	s := NowISO()
	if len(s) < 20 {
		t.Errorf("NowISO() = %q, too short", s)
	}
	if s[len(s)-1] != 'Z' {
		t.Errorf("NowISO() should end with Z, got %q", s)
	}
}

func TestIsRecent(t *testing.T) {
	now := NowISO()
	if !IsRecent(now, 1) {
		t.Error("now should be recent within 1 day")
	}
	if IsRecent("2020-01-01T00:00:00.000Z", 1) {
		t.Error("2020 should not be recent")
	}
	if IsRecent("invalid", 1) {
		t.Error("invalid timestamp should return false")
	}
}
