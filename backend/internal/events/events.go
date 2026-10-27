// Package events 领域事件（M6）：单据关键动作写入投递队列，后台 worker 异步推送给订阅方。
package events

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/model"
)

// Publish 发布领域事件（不影响业务事务结果：仅入队）。
func Publish(db *gorm.DB, tenantID int64, event string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	var eps []model.WebhookEndpoint
	db.Where("tenant_id=? AND status=1", tenantID).Find(&eps)
	for _, ep := range eps {
		if ep.Events != "*" && !strings.Contains(","+ep.Events+",", ","+event+",") {
			continue
		}
		db.Create(&model.WebhookDelivery{
			TenantID: tenantID, EndpointID: ep.ID, Event: event,
			Payload: string(body), Status: "pending", CreatedAt: time.Now(),
		})
	}
}

// StartWorker 后台投递：每 3 秒扫描 pending，指数退避由 Attempts 控制（5 次放弃）。
func StartWorker(db *gorm.DB) {
	go func() {
		for {
			var rows []model.WebhookDelivery
			db.Where("status='pending' AND attempts < 5").Limit(50).Find(&rows)
			for _, d := range rows {
				var ep model.WebhookEndpoint
				if db.First(&ep, d.EndpointID).Error != nil {
					db.Model(&model.WebhookDelivery{}).Where("id=?", d.ID).Updates(map[string]any{"status": "failed", "http_code": 0})
					continue
				}
				code, err := postJSON(ep.URL, ep.Secret, d.Event, d.Payload)
				st := "ok"
				if err != nil || code >= 300 {
					st = "pending"
					if d.Attempts+1 >= 5 {
						st = "failed"
					}
				}
				now := time.Now()
				ups := map[string]any{"attempts": d.Attempts + 1, "http_code": code, "status": st}
				if st == "ok" {
					ups["delivered_at"] = &now
				}
				db.Model(&model.WebhookDelivery{}).Where("id=?", d.ID).Updates(ups)
			}
			time.Sleep(3 * time.Second)
		}
	}()
	log.Println("[webhook-worker] started")
}

func postJSON(url, secret, event, payload string) (int, error) {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Evolvermon-Event", event)
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(payload))
		req.Header.Set("X-Evolvermon-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

var _ = fmt.Sprintf
