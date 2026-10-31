// Package fanout webhook 扇出拉取（Phase 2 分库）：integration 进程定时拉取 core outbox，
// 在自家库按 endpoints 生成投递行；messageId 幂等（consumed_messages）。
package fanout

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/outbox"
)

// Start 启动拉取循环（coreURL 为 core 服务基址；后台 goroutine）。
func Start(db *gorm.DB, coreURL string) {
	go func() {
		hc := &http.Client{Timeout: 5 * time.Second}
		token := os.Getenv("EM_INTERNAL_TOKEN")
		for {
			pullOnce(db, hc, coreURL, token)
			time.Sleep(3 * time.Second)
		}
	}()
	log.Println("[webhook-fanout] pull loop started ->", coreURL)
}

func pullOnce(db *gorm.DB, hc *http.Client, coreURL, token string) {
	var cur model.IntegrationPullCursor
	if err := db.First(&cur, 1).Error; err != nil {
		cur = model.IntegrationPullCursor{ID: 1}
		db.Create(&cur)
	}
	for {
		req, _ := http.NewRequest(http.MethodGet,
			fmt.Sprintf("%s/internal/core/events?after=%d&limit=100", coreURL, cur.LastID), nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := hc.Do(req)
		if err != nil {
			log.Printf("[webhook-fanout] pull: %v", err)
			return
		}
		var out struct {
			Data struct {
				List []model.OutboxMessage `json:"list"`
			} `json:"data"`
		}
		err = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 {
			log.Printf("[webhook-fanout] decode: %v http=%d", err, resp.StatusCode)
			return
		}
		if len(out.Data.List) == 0 {
			return
		}
		for _, m := range out.Data.List {
			if !outbox.MarkConsumed(db, "webhook-fanout", m.MessageID) {
				continue // 已扇出过（幂等）
			}
			createDeliveries(db, m)
			cur.LastID = m.ID
		}
		db.Model(&model.IntegrationPullCursor{}).Where("id=1").Update("last_id", cur.LastID)
		if len(out.Data.List) < 100 {
			return
		}
	}
}

// createDeliveries 按租户 endpoints 生成投递行（events.Publish 原扇出逻辑等价迁移）。
func createDeliveries(db *gorm.DB, m model.OutboxMessage) {
	var eps []model.WebhookEndpoint
	db.Where("tenant_id=? AND status=1", m.TenantID).Find(&eps)
	for _, ep := range eps {
		if ep.Events != "*" && !strings.Contains(","+ep.Events+",", ","+m.Event+",") {
			continue
		}
		db.Create(&model.WebhookDelivery{
			TenantID: m.TenantID, EndpointID: ep.ID, Event: m.Event,
			Payload: m.Payload, Status: "pending", CreatedAt: time.Now(),
		})
	}
}
