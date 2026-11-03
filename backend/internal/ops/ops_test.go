package ops

import (
	"os"
	"path/filepath"
	"testing"
)

// registry.yml 解析（真实注册表文件快照）。
func TestLoadRegistry(t *testing.T) {
	path := filepath.Join("..", "..", "ops", "registry.yml")
	defs, err := LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
		if d.URL == "" {
			t.Errorf("service %s 缺 url", d.Name)
		}
	}
	for _, want := range []string{"gateway", "identity", "core", "integration", "scheduler"} {
		if !names[want] {
			t.Errorf("registry 缺服务 %s", want)
		}
	}
}

// 临时注册表：driver 类型校验。
func TestLoadRegistryTemp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "r.yml")
	os.WriteFile(p, []byte("services:\n  - name: a\n    url: http://x\n    driver: process\n    start: ./a\n  - name: b\n    url: http://y\n    driver: compose\n    composeProject: em\n"), 0o644)
	defs, err := LoadRegistry(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 2 || defs[0].Driver != "process" || defs[1].ComposeProject != "em" {
		t.Errorf("解析异常: %+v", defs)
	}
}

// 自研 Prometheus 文本格式解析。
func TestParseMetrics(t *testing.T) {
	body := `# TYPE em_go_goroutines gauge
em_go_goroutines 8
em_go_mem_heap_bytes 123456
em_uptime_seconds 300
em_http_requests_total{method="POST",route="/api/v1/x",status="2xx"} 41
em_http_requests_total{method="GET",route="/api/v1/y",status="2xx"} 9
em_http_request_duration_seconds_bucket{le="0.05",method="POST",route="/api/v1/x"} 40
`
	s := parseMetrics(body)
	if s.goros != 8 || s.heap != 123456 || s.uptime != 300 {
		t.Errorf("基础指标解析错误: %+v", s)
	}
	if s.reqs != 50 {
		t.Errorf("requests_total 累加 = %v, want 50", s.reqs)
	}
	if s.p99 != 0.05 {
		t.Errorf("p99 桶 = %v, want 0.05", s.p99)
	}
}

// none 驱动拒绝控制（fail-fast 语义）。
func TestNoneDriver(t *testing.T) {
	d := NewDriver("none")
	if d.Kind() != "none" {
		t.Fatalf("kind = %s", d.Kind())
	}
	if err := d.Start(nil, ServiceDef{Name: "gateway"}); err == nil {
		t.Error("none driver 应拒绝 Start")
	}
	if err := d.Stop(nil, ServiceDef{Name: "gateway"}); err == nil {
		t.Error("none driver 应拒绝 Stop")
	}
}

// Phase 4 ①：规则引擎——down/p99/goroutine 触发与去重。
func TestAlertRules(t *testing.T) {
	st := ServiceState{ServiceDef: ServiceDef{Name: "x", URL: "http://x"}, Status: "down"}
	for _, r := range alertRules {
		if r.Name == "service_down" {
			hit, detail := r.Check(&st, nil)
			if !hit || detail == "" {
				t.Fatalf("service_down 未触发: %v %q", hit, detail)
			}
		}
	}
	st2 := ServiceState{ServiceDef: ServiceDef{Name: "x"}, Status: "up", P99Bucket: 0.8}
	for _, r := range alertRules {
		if r.Name == "p99_high" {
			if hit, _ := r.Check(&st2, nil); !hit {
				t.Fatal("p99_high 未触发（0.8s>500ms 默认阈值）")
			}
		}
	}
	st3 := ServiceState{ServiceDef: ServiceDef{Name: "x"}, Status: "up", P99Bucket: 0.01}
	for _, r := range alertRules {
		if r.Name == "p99_high" {
			if hit, _ := r.Check(&st3, nil); hit {
				t.Fatal("p99_high 误触发（10ms）")
			}
		}
	}
	st4 := ServiceState{ServiceDef: ServiceDef{Name: "x"}, Status: "up", Goroutines: 5000}
	for _, r := range alertRules {
		if r.Name == "goroutine_leak" {
			if hit, _ := r.Check(&st4, nil); !hit {
				t.Fatal("goroutine_leak 未触发（5000>2000）")
			}
		}
	}
}

func TestEnvFloat(t *testing.T) {
	t.Setenv("EM_ALERT_P99_MS", "250")
	if got := envFloat("EM_ALERT_P99_MS", 500); got != 250 {
		t.Fatalf("envFloat=%v want 250", got)
	}
	if got := envFloat("EM_ALERT_MISSING_X", 500); got != 500 {
		t.Fatalf("默认=%v want 500", got)
	}
}
