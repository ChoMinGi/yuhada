package util

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// NowISO — 스키마 내부에서 사용하는 ISO8601 with milliseconds + Z
// SQLite strftime('%Y-%m-%dT%H:%M:%fZ', 'now') 과 동일 형식.
func NowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// IsRecent — ISO 타임스탬프가 days일 이내인지.
func IsRecent(iso string, days int) bool {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		// 스키마 기본 형식 fallback
		t, err = time.Parse("2006-01-02T15:04:05.000Z", iso)
		if err != nil {
			return false
		}
	}
	return time.Since(t) < time.Duration(days)*24*time.Hour
}

// FormatKRW — 123456 → "₩123,456"
func FormatKRW(v int64) string {
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	s := fmt.Sprintf("%d", v)
	var out strings.Builder
	out.WriteString("₩")
	out.WriteString(sign)
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

// NormalizePhone — "+82 10-1234-5678" / "010-1234-5678" → "01012345678"
func NormalizePhone(s string) string {
	digits := regexp.MustCompile(`\D`).ReplaceAllString(s, "")
	// +82로 시작하면 10 prefix 복원
	if strings.HasPrefix(digits, "82") && len(digits) >= 11 {
		digits = "0" + digits[2:]
	}
	return digits
}

// FormatPhone — "01012345678" → "010-1234-5678"
func FormatPhone(raw string) string {
	d := NormalizePhone(raw)
	switch len(d) {
	case 11:
		return fmt.Sprintf("%s-%s-%s", d[:3], d[3:7], d[7:])
	case 10:
		return fmt.Sprintf("%s-%s-%s", d[:3], d[3:6], d[6:])
	default:
		return raw
	}
}
