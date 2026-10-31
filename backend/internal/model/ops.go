// Package model — ops 域（ops-admin 统一管理平台，Phase 1.5）。
// 仅 ops-admin 服务读写；ops schema（Phase 0.2 已预留）。
package model

import "time"

// OpsAuditLog 运维审计（append-only）：谁在何时对哪个服务做了什么、结果如何。
type OpsAuditLog struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	UserID    int64     `json:"userId"`
	Username  string    `gorm:"size:64" json:"username"`
	Service   string    `gorm:"size:32;index" json:"service"` // 服务名或 "registry"
	Action    string    `gorm:"size:16" json:"action"`         // start|stop|restart|scale
	Result    string    `gorm:"size:16" json:"result"`         // ok|failed
	Detail    string    `gorm:"size:512" json:"detail"`
	CreatedAt time.Time `json:"createdAt"`
}

// OpsAlert 告警（Phase 1 v1：服务不可用检测；open→resolved 生命周期）。
type OpsAlert struct {
	ID          int64      `gorm:"primaryKey" json:"id"`
	Service     string     `gorm:"size:32;index" json:"service"`
	Rule        string     `gorm:"size:64" json:"rule"` // service_down | http_5xx
	Detail      string     `gorm:"size:512" json:"detail"`
	OpenedAt    time.Time  `json:"openedAt"`
	ResolvedAt  *time.Time `json:"resolvedAt"`
}
