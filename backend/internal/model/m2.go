package model

import "time"

// ---------------- M2：序列号追溯（数据库设计 v0.2 增补） ----------------

// SerialNumber 序列号台账：完工报交时为启用序列号管理的物料逐件生成（FR-MES 序列号 v1）。
type SerialNumber struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	TenantID     int64     `gorm:"index;not null" json:"tenantId"`
	FactoryID    int64     `gorm:"index" json:"factoryId"`
	MaterialID   int64     `gorm:"index" json:"materialId"`
	Serial       string    `gorm:"uniqueIndex:uq_serial;size:64" json:"serial"`
	BatchNo      string    `gorm:"size:64;index" json:"batchNo"`
	WorkOrderID  int64     `gorm:"index" json:"workOrderId"`
	WorkOrderNo  string    `gorm:"size:32" json:"workOrderNo"`
	StockOrderID int64     `json:"stockOrderId"` // 最近一次出入库单
	Status       string    `gorm:"size:16;default:'in_stock';index" json:"status"` // in_stock | out | scrapped
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}
