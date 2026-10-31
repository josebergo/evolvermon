// Package model — 事件一致性底座（微服务方案 §2.4 铁律 3，Phase 2.2）。
// OutboxMessage：事务性投递箱（与业务同库同事务写入；relay 异步推送到 Broker）。
// ConsumedMessage：消费幂等表（按 consumer+message_id 去重，跨服务事件消费防重）。
// Phase 2 共库阶段统一落 platform schema；Phase 3 拆库时各服务在自己库内建同名表。
package model

import "time"

// OutboxMessage 通用事件投递箱行。
type OutboxMessage struct {
	ID        int64      `gorm:"primaryKey" json:"id"`
	MessageID string     `gorm:"size:36;uniqueIndex" json:"messageId"` // uuid，消费方幂等键
	TenantID  int64      `gorm:"index" json:"tenantId"`
	Event     string     `gorm:"size:64;index" json:"event"`
	Payload   string     `gorm:"type:text" json:"payload"`
	Attempts  int        `gorm:"default:0" json:"attempts"`
	SentAt    *time.Time `json:"sentAt"` // NULL=未投递
	CreatedAt time.Time  `json:"createdAt"`
}

// ConsumedMessage 消费幂等记录（唯一约束 consumer+message_id）。
type ConsumedMessage struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	Consumer    string    `gorm:"size:64;uniqueIndex:idx_consumer_msg" json:"consumer"`
	MessageID   string    `gorm:"size:36;uniqueIndex:idx_consumer_msg" json:"messageId"`
	ConsumedAt  time.Time `json:"consumedAt"`
}
