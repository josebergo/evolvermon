package model

import "time"

// ---------------- M6：事件出口（Webhook）、设备遥测 ----------------

type WebhookEndpoint struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	TenantID  int64     `gorm:"index;not null" json:"tenantId"`
	Name      string    `gorm:"size:64" json:"name"`
	URL       string    `gorm:"size:256" json:"url"`
	Secret    string    `gorm:"size:128" json:"secret"` // HMAC-SHA256 签名密钥
	Events    string    `gorm:"size:512" json:"events"` // 逗号分隔订阅事件，"*" 全部
	Status    int16     `gorm:"default:1" json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type WebhookDelivery struct {
	ID         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID   int64     `gorm:"index" json:"tenantId"`
	EndpointID int64     `gorm:"index" json:"endpointId"`
	Event      string    `gorm:"size:64;index" json:"event"`
	Payload    string    `gorm:"type:jsonb;default:'{}'" json:"payload"`
	Status     string    `gorm:"size:16;default:'pending';index" json:"status"` // pending | ok | failed
	HTTPCode   int       `json:"httpCode"`
	Attempts   int       `gorm:"default:0" json:"attempts"`
	CreatedAt  time.Time `gorm:"index" json:"createdAt"`
	DeliveredAt *time.Time `json:"deliveredAt"`
}

type DeviceTelemetry struct {
	ID         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID   int64     `gorm:"index" json:"tenantId"`
	FactoryID  int64     `gorm:"index" json:"factoryId"`
	DeviceCode string    `gorm:"size:64;index" json:"deviceCode"`
	Metric     string    `gorm:"size:64" json:"metric"` // count | temperature | oee ...
	Value      float64   `gorm:"type:numeric(18,4)" json:"value"`
	WorkCenter string    `gorm:"size:64" json:"workCenter"`
	ReportedAt time.Time `gorm:"index" json:"reportedAt"`
}
