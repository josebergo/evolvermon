package metrics

import (
	"strings"
	"testing"
)

func TestRenderPromText(t *testing.T) {
	r := &Registry{counters: map[string]*Counter{}, histogram: &Histogram{rows: map[string]*histRow{}}, uptime: startAt}
	r.Inc(reqTotalName, map[string]string{"method": "POST", "route": "/api/v1/x", "status": "2xx"})
	r.Observe(reqDurName, map[string]string{"method": "POST", "route": "/api/v1/x"}, 0.03)

	out := r.Render()
	for _, want := range []string{
		"# TYPE em_http_requests_total counter",
		`em_http_requests_total{method="POST",route="/api/v1/x",status="2xx"} 1`,
		"# TYPE em_http_request_duration_seconds histogram",
		`em_http_request_duration_seconds_bucket{le="0.05",method="POST",route="/api/v1/x"} 1`,
		`em_http_request_duration_seconds_bucket{le="+Inf",method="POST",route="/api/v1/x"} 1`,
		`em_http_request_duration_seconds_count{method="POST",route="/api/v1/x"} 1`,
		"em_go_goroutines",
		"em_uptime_seconds",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q\nout:\n%s", want, out)
		}
	}
}

func TestQuantile(t *testing.T) {
	r := &Registry{counters: map[string]*Counter{}, histogram: &Histogram{rows: map[string]*histRow{}}, uptime: startAt}
	labels := map[string]string{"route": "/x"}
	for i := 0; i < 100; i++ {
		r.Observe(reqDurName, labels, 0.001) // 100 个 1ms 样本
	}
	if got := r.Quantile(reqDurName, labels, 0.99); got != 0.001 {
		t.Errorf("p99 = %v, want 0.001", got)
	}
	if got := r.Quantile(reqDurName, labels, 0.999); got != 0.001 {
		t.Errorf("p999 = %v, want 0.001（仍在最小桶内）", got)
	}
}
