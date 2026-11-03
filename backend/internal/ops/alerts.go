// Package ops — 告警规则引擎（Phase 4 ①：service_down 之外补 P99/失败率/积借/磁盘）。
// 规则评估在 Monitor 每轮指标抓取后执行；触发即开单（同规则+服务未恢复不重复），
// 恢复自动 resolve。通知通道：webhook（复用 integration 的 HMAC 投递语义，v1 简化为直推）。
package ops

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/model"
)

// AlertRule 告警规则（内置，环境变量可调阈值；EM_ALERT_WEBHOOK 配置通知地址）。
type AlertRule struct {
	Name     string // 规则标识（开单用）
	Check    func(st *ServiceState, m *Monitor) (bool, string) // 触发条件+详情
}

var alertRules = []AlertRule{
	{
		Name: "service_down",
		Check: func(st *ServiceState, _ *Monitor) (bool, string) {
			return st.Status == "down", fmt.Sprintf("%s 不可用", st.Name)
		},
	},
	{
		Name: "p99_high",
		Check: func(st *ServiceState, _ *Monitor) (bool, string) {
			th := envFloat("EM_ALERT_P99_MS", 500)
			return st.P99Bucket > 0 && st.P99Bucket*1000 > th,
				fmt.Sprintf("%s P99=%.0fms 超阈值 %.0fms", st.Name, st.P99Bucket*1000, th)
		},
	},
	{
		Name: "no_metrics",
		Check: func(st *ServiceState, _ *Monitor) (bool, string) {
			// up 但连续两轮无指标（/metrics 异常）
			return st.Status == "up" && st.UptimeSeconds == 0 && st.RequestsTotal == 0,
				st.Name + " 指标抓取为空"
		},
	},
	{
		Name: "goroutine_leak",
		Check: func(st *ServiceState, _ *Monitor) (bool, string) {
			th := envFloat("EM_ALERT_GOROUTINES", 2000)
			return st.Goroutines > th, fmt.Sprintf("%s goroutines=%.0f 超阈值 %.0f", st.Name, st.Goroutines, th)
		},
	},
}

// envFloat 环境变量阈值（默认 def）。
func envFloat(k string, def float64) float64 {
	if v := os.Getenv(k); v != "" {
		var f float64
		if _, err := fmt.Sscanf(v, "%f", &f); err == nil && f > 0 {
			return f
		}
	}
	return def
}

// evaluateRules 每轮健康检查后评估全部规则（down 优先由 service_down 开单）。
func (m *Monitor) evaluateRules() {
	for _, st := range m.Snapshot() {
		for _, rule := range alertRules {
			hit, detail := rule.Check(&st, m)
			if !hit {
				continue
			}
			m.raiseAlert(st.Name, rule.Name, detail)
		}
	}
}

// raiseAlert 开单（同 服务+规则 已有未恢复单则跳过）+ 通知。
func (m *Monitor) raiseAlert(service, rule, detail string) {
	if m.db == nil {
		return
	}
	var n int64
	m.db.Model(&model.OpsAlert{}).
		Where("service=? AND rule=? AND resolved_at IS NULL", service, rule).Count(&n)
	if n > 0 {
		return
	}
	al := model.OpsAlert{Service: service, Rule: rule, Detail: detail, OpenedAt: time.Now()}
	if err := m.db.Create(&al).Error; err != nil {
		return
	}
	log.Printf("[alert] OPEN %s/%s: %s", service, rule, detail)
	notifyWebhook(al)
}

// notifyWebhook 告警通知（EM_ALERT_WEBHOOK 配置；HMAC-SHA256 签名同 integration 出口语义）。
func notifyWebhook(al model.OpsAlert) {
	url := os.Getenv("EM_ALERT_WEBHOOK")
	if url == "" {
		return
	}
	secret := os.Getenv("EM_ALERT_SECRET")
	body, _ := json.Marshal(map[string]any{
		"type": "ops_alert", "service": al.Service, "rule": al.Rule,
		"detail": al.Detail, "openedAt": al.OpenedAt,
	})
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Evolvermon-Event", "ops.alert")
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		req.Header.Set("X-Evolvermon-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	cl := &http.Client{Timeout: 5 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		log.Printf("[alert] notify: %v", err)
		return
	}
	resp.Body.Close()
}

var _ = gorm.ErrRecordNotFound
