// Package outbox 事务性事件投递箱 + 消费幂等（微服务方案 §2.4 铁律 3，Phase 2.2）。
//
// 发布侧：Save 与业务同事务写入（调用方传 tx）；relay 异步推 Broker 并标记 SentAt，
// 失败重试（Attempts 计数，超限告警留痕不阻塞）。至少一次投递。
// 消费侧：MarkConsumed 以 consumer+message_id 唯一约束去重——重复消息返回 false 直接丢弃。
// Lite 共库形态下 relay 由聚合进程运行；Micro 形态由 integration 进程运行（见 cmd/integration）。
package outbox

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"evolvermon/backend/internal/infra"
	"evolvermon/backend/internal/model"
)

// Save 写入投递箱（应在业务事务内调用，传 tx 保证原子）。
func Save(tx *gorm.DB, tenantID int64, event string, payload []byte) (string, error) {
	msgID := uuid.NewString()
	return msgID, tx.Create(&model.OutboxMessage{
		MessageID: msgID, TenantID: tenantID, Event: event,
		Payload: string(payload), CreatedAt: time.Now(),
	}).Error
}

// StartRelay 后台投递循环：扫描未发送行 → 推 Broker（Key=tenant-{id} 保序）→ 标记 SentAt。
// 每轮 3 秒；单批 100 条；失败 Attempts+1 超过 5 次置 sent_at=now（放弃投递，留痕对账）。
func StartRelay(db *gorm.DB, broker infra.Broker) {
	go func() {
		for {
			relayOnce(db, broker)
			time.Sleep(3 * time.Second)
		}
	}()
	log.Println("[outbox-relay] started")
}

func relayOnce(db *gorm.DB, broker infra.Broker) {
	var rows []model.OutboxMessage
	db.Where("sent_at IS NULL AND attempts < 5").Order("id").Limit(100).Find(&rows)
	for _, r := range rows {
		err := broker.Publish(context.Background(), infra.Message{
			ID: r.MessageID, Topic: infra.TopicEvents,
			Key: tenantKey(r.TenantID), TenantID: r.TenantID,
			Event: r.Event, Payload: []byte(r.Payload), Timestamp: time.Now(),
		})
		now := time.Now()
		if err != nil {
			db.Model(&model.OutboxMessage{}).Where("id=?", r.ID).UpdateColumn("attempts", r.Attempts+1)
			log.Printf("[outbox-relay] publish %s: %v", r.Event, err)
			continue
		}
		db.Model(&model.OutboxMessage{}).Where("id=?", r.ID).Updates(map[string]any{"sent_at": &now})
	}
}

// MarkConsumed 消费幂等：首次消费返回 true，重复返回 false（调用方应跳过处理）。
func MarkConsumed(db *gorm.DB, consumer, messageID string) bool {
	if err := db.Create(&model.ConsumedMessage{Consumer: consumer, MessageID: messageID, ConsumedAt: time.Now()}).Error; err != nil {
		return false // 唯一约束冲突 = 已消费过
	}
	return true
}

// Stats 对账用：返回 (未投递数, 已投递数, 放弃数)。
func Stats(db *gorm.DB) (pending, sent, dead int64) {
	db.Model(&model.OutboxMessage{}).Where("sent_at IS NULL AND attempts < 5").Count(&pending)
	db.Model(&model.OutboxMessage{}).Where("sent_at IS NOT NULL").Count(&sent)
	db.Model(&model.OutboxMessage{}).Where("sent_at IS NULL AND attempts >= 5").Count(&dead)
	return
}

func tenantKey(id int64) string {
	// 与 events.publishBus 同构：同租户事件在总线内保序
	return "tenant-" + strconv.FormatInt(id, 10)
}
