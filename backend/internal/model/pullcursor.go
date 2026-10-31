// Package model — integration 域：webhook 扇出拉取游标（Phase 2 分库：integration 库内记录已拉取的 core outbox 位点）。
package model

import "time"

// IntegrationPullCursor 单行游标（id=1）。
type IntegrationPullCursor struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	LastID    int64     `json:"lastId"` // 已拉取的 core outbox_messages.id
	UpdatedAt time.Time `json:"updatedAt"`
}
