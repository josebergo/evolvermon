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

	"evolvermon/backend/internal/model"
	"evolvermon/backend/internal/outbox"
)

// Start 启动拉取循环（coreURL 为 core 服务基址；后台 goroutine）。
func Start(db *gorm.DB, coreURL string) {
	// 三库拆分：erp/mes/wms 各自 outbox；独立游标行（id=1 core/2 erp/3 mes/4 wms），按 (库,id) 幂等
	baseURLs := []string{coreURL}
	for _, u := range []string{os.Getenv("EM_ERP_URL"), os.Getenv("EM_MES_URL"), os.Getenv("EM_WMS_URL")} {
		if u != "" && u != coreURL {
			baseURLs = append(baseURLs, u)
		}
	}
	for i, u := range baseURLs {
		go func(idx int, base string) {
			hc := &http.Client{Timeout: 5 * time.Second}
			token := os.Getenv("EM_INTERNAL_TOKEN")
			for {
				pullOnceAt(db, hc, base, token, int64(idx+1))
				time.Sleep(3 * time.Second)
			}
		}(i, u)
	}
	log.Println("[webhook-fanout] pull loops started:", len(baseURLs))
}

// pullOnceAt 指定游标行拉取（cursorID 区分来源库）。
func pullOnceAt(db *gorm.DB, hc *http.Client, coreURL, token string, cursorID int64) {
	var cur model.IntegrationPullCursor
	if err := db.First(&cur, cursorID).Error; err != nil {
		cur = model.IntegrationPullCursor{ID: cursorID}
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
				continue
			}
			createDeliveries(db, m)
			cur.LastID = m.ID
		}
		db.Model(&model.IntegrationPullCursor{}).Where("id=?", cursorID).Update("last_id", cur.LastID)
		if len(out.Data.List) < 100 {
			return
		}
	}
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
