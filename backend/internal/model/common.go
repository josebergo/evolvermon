package model

import (
	"time"

	"gorm.io/gorm"
)

// BaseModel 所有租户业务表的通用列（数据库设计文档 §0）。
type BaseModel struct {
	ID        int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID  int64          `gorm:"index:idx_tenant;not null;default:0" json:"tenantId"`
	FactoryID *int64         `gorm:"index;default:null" json:"factoryId"`
	CreatedBy int64          `json:"createdBy"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedBy int64          `json:"updatedBy"`
	UpdatedAt time.Time      `json:"updatedAt"`
	Version   int            `gorm:"default:1" json:"version"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// GetBase 供 crud 引擎定位嵌入的审计列（方法提升到所有业务模型）。
func (b *BaseModel) GetBase() *BaseModel { return b }
