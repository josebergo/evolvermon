package middleware

import "testing"

// Phase 0.4：W3C traceparent 解析——合法延续、非法重新生成。
func TestParseTraceparent(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "4bf92f3577b34da6a3ce929d0e0e4736"},
		{"", ""},
		{"garbage", ""},
		{"00-short-span-01", ""},
		{"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7", ""}, // 缺 flags 段
	}
	for _, c := range cases {
		if got := parseTraceparent(c.in); got != c.want {
			t.Errorf("parse(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
