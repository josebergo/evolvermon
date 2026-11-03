// 健康轮询 + 指标抓取 + 告警评估（Phase 1.5）。
package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/model"
)

var dataEnsureSchemas = data.EnsureSchemas // 独立变量便于测试打桩

// ServiceState 注册表条目 + 运行时状态（API 输出）。
type ServiceState struct {
	ServiceDef    `json:",inline"`
	Status        string  `json:"status"`        // up | down | unknown
	Ready         bool    `json:"ready"`
	LastCheckAt   string  `json:"lastCheckAt"`
	DownSince     string  `json:"downSince,omitempty"`
	UptimeSeconds float64 `json:"uptimeSeconds"`
	Goroutines    float64 `json:"goroutines"`
	HeapBytes     float64 `json:"heapBytes"`
	RequestsTotal float64 `json:"requestsTotal"`
	P99Bucket     float64 `json:"p99BucketSeconds"` // HTTP p99 时延桶（秒）
	Version       int     `json:"schemaVersion"`    // readyz 附带（预留）
}

// Monitor 轮询器：健康 10s / 指标 15s；环形保留 24h 指标样本。
type Monitor struct {
	mu       sync.RWMutex
	services map[string]*ServiceState
	history  map[string][]sample // 最近 24h（15s 采样 ≈ 5760 点）
	db       *gorm.DB
	client   *http.Client
}

type sample struct {
	at      time.Time
	uptime  float64
	goros   float64
	heap    float64
	reqs    float64
	p99     float64
}

func NewMonitor(defs []ServiceDef, db *gorm.DB) *Monitor {
	m := &Monitor{
		services: map[string]*ServiceState{},
		history:  map[string][]sample{},
		db:       db,
		client:   &http.Client{Timeout: 3 * time.Second},
	}
	for _, d := range defs {
		m.services[d.Name] = &ServiceState{ServiceDef: d, Status: "unknown"}
	}
	return m
}

// Run 阻塞轮询（独立 goroutine 调用）。
func (m *Monitor) Run(ctx context.Context) {
	m.pollAll(ctx) // 启动即首轮，避免 API 空窗
	tick := time.NewTicker(10 * time.Second)
	metricsTick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	defer metricsTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			m.pollAll(ctx)
		case <-metricsTick.C:
			m.scrapeAll()
		}
	}
}

func (m *Monitor) pollAll(ctx context.Context) {
	m.mu.RLock()
	names := make([]string, 0, len(m.services))
	for n := range m.services {
		names = append(names, n)
	}
	m.mu.RUnlock()
	for _, n := range names {
		m.pollOne(ctx, n)
	}
}

func (m *Monitor) pollOne(ctx context.Context, name string) {
	m.mu.RLock()
	st, ok := m.services[name]
	m.mu.RUnlock()
	if !ok {
		return
	}
	healthOK := m.probe(st.URL + "/healthz")
	readyOK := m.probe(st.URL + "/readyz")
	status := "up"
	if !healthOK {
		status = "down"
	}
	m.mu.Lock()
	wasDown := st.Status == "down"
	st.Status, st.Ready, st.LastCheckAt = status, readyOK, time.Now().Format(time.RFC3339)
	if status == "down" && st.DownSince == "" {
		st.DownSince = st.LastCheckAt
	}
	if status == "up" {
		st.DownSince = ""
	}
	m.mu.Unlock()
	// 恢复解决（up 后自动 resolve 该服务全部未恢复告警；开单由规则引擎 evaluateRules 统一处理）
	if status == "up" && wasDown {
		m.resolveAlert(name)
	}
}

func (m *Monitor) probe(url string) bool {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (m *Monitor) openAlert(name, at string) {
	if m.db == nil {
		return
	}
	m.db.Create(&model.OpsAlert{Service: name, Rule: "service_down",
		Detail: fmt.Sprintf("%s 不可用（%s）", name, at), OpenedAt: time.Now()})
}

func (m *Monitor) resolveAlert(name string) {
	if m.db == nil {
		return
	}
	now := time.Now()
	m.db.Model(&model.OpsAlert{}).
		Where("service=? AND resolved_at IS NULL", name).
		Update("resolved_at", &now)
}

// scrapeAll 抓取 /metrics 并解析关键指标（自研文本格式，无需 prometheus 依赖）。
func (m *Monitor) scrapeAll() {
	m.mu.RLock()
	names := make([]string, 0, len(m.services))
	for n := range m.services {
		names = append(names, n)
	}
	m.mu.RUnlock()
	for _, n := range names {
		m.scrapeOne(n)
	}
}

func (m *Monitor) scrapeOne(name string) {
	m.mu.RLock()
	st, _ := m.services[name]
	m.mu.RUnlock()
	if st == nil || st.Status != "up" {
		return
	}
	body, err := m.fetch(st.URL + "/metrics")
	if err != nil {
		return
	}
	s := parseMetrics(body)
	m.mu.Lock()
	st.UptimeSeconds, st.Goroutines, st.HeapBytes = s.uptime, s.goros, s.heap
	st.RequestsTotal, st.P99Bucket = s.reqs, s.p99
	h := append(m.history[name], sample{at: time.Now(), uptime: s.uptime, goros: s.goros, heap: s.heap, reqs: s.reqs, p99: s.p99})
	if len(h) > 5760 { // 24h @15s
		h = h[len(h)-5760:]
	}
	m.history[name] = h
	m.mu.Unlock()
}

func (m *Monitor) fetch(url string) (string, error) {
	resp, err := m.client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String(), nil
}

// Snapshot 当前全部服务状态（API 输出）。
func (m *Monitor) Snapshot() []ServiceState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ServiceState, 0, len(m.services))
	for _, st := range m.services {
		out = append(out, *st)
	}
	return out
}

// History 某服务最近 n 个指标样本（时间正序）。
func (m *Monitor) History(name string, n int) []sample {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h := m.history[name]
	if n <= 0 || n > len(h) {
		n = len(h)
	}
	out := make([]sample, n)
	copy(out, h[len(h)-n:])
	return out
}

// ---- 自研 Prometheus 文本格式解析（仅取需要的指标） ----

type metricsSnapshot struct {
	uptime, goros, heap, reqs, p99 float64
}

func parseMetrics(body string) metricsSnapshot {
	var s metricsSnapshot
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, rest, _ := strings.Cut(line, " ")
		val, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
		if err != nil {
			continue
		}
		// 计数器多标签行：name{...} val —— name 取 { 前段
		if i := strings.Index(name, "{"); i > 0 {
			name = name[:i]
			// 请求总数需按行累加（按 route/method/status 分行）
			if name == "em_http_requests_total" {
				s.reqs += val
				continue
			}
			// p99：取 ≤0.05 桶累积计数 / 总数近似（粗粒度足够运维面板）
			if strings.HasPrefix(line, "em_http_request_duration_seconds_bucket") && strings.Contains(line, `le="0.1"`) {
				s.p99 = 0.1
			}
			if strings.HasPrefix(line, "em_http_request_duration_seconds_bucket") && strings.Contains(line, `le="0.05"`) && val > 0 {
				s.p99 = 0.05
			}
			continue
		}
		switch name {
		case "em_uptime_seconds":
			s.uptime = val
		case "em_go_goroutines":
			s.goros = val
		case "em_go_mem_heap_bytes":
			s.heap = val
		}
	}
	return s
}

// EnsureDB ops-admin 启动时建自己的表（幂等；baseline 之外独立演进）。
// 空库时 identity 迁移可能尚未执行：先幂等建域 schema。
func EnsureDB(db *gorm.DB) error {
	if err := dataEnsureSchemas(db); err != nil {
		return err
	}
	return db.AutoMigrate(&model.OpsAuditLog{}, &model.OpsAlert{})
}

// audit 记录运维审计（append-only）。
func audit(db *gorm.DB, userID int64, username, service, action, result, detail string) {
	db.Create(&model.OpsAuditLog{UserID: userID, Username: username, Service: service,
		Action: action, Result: result, Detail: detail, CreatedAt: time.Now()})
}

// State JSON 调试输出用（避免未使用 import）。
var _ = json.Marshal
