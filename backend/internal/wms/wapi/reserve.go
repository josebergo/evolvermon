// wms 预占写通道（Phase 3 ③B-①）：erp 审核建预占/作废释放经此；幂等键=sales_order+SO id。
package wapi

import (
	"time"

	"gorm.io/gorm"

	"evolvermon/backend/internal/model"
)

// ReserveLine 预占行。
type ReserveLine struct {
	MaterialID int64   `json:"materialId"`
	Qty        float64 `json:"qty"`
	SourceLineID int64 `json:"sourceLineId"`
}

// ReserveReq 建预占请求（幂等：同 source 键存在 held 行则跳过）。
type ReserveReq struct {
	TenantID  int64  `json:"tenantId"`
	FactoryID int64  `json:"factoryId"`
	BizType   string `json:"bizType"`
	SourceDocType string `json:"sourceDocType"`
	SourceDocID   int64  `json:"sourceDocId"`
	Lines     []ReserveLine `json:"lines"`
}

// Reserve 幂等建预占（存在即复用）。
func Reserve(db *gorm.DB, req ReserveReq) error {
	var n int64
	db.Model(&model.StockReservation{}).
		Where("source_doc_type=? AND source_doc_id=? AND status='held'", req.SourceDocType, req.SourceDocID).
		Count(&n)
	if n > 0 {
		return nil // 幂等：已有预占
	}
	for _, l := range req.Lines {
		r := model.StockReservation{
			TenantID: req.TenantID, FactoryID: req.FactoryID, MaterialID: l.MaterialID,
			Qty: l.Qty, BizType: req.BizType, SourceDocType: req.SourceDocType,
			SourceDocID: req.SourceDocID, SourceLineID: l.SourceLineID,
		}
		if err := db.Create(&r).Error; err != nil {
			return err
		}
	}
	return nil
}

// ReleaseReq 释放预占请求。
type ReleaseReq struct {
	SourceDocType string `json:"sourceDocType"`
	SourceDocID   int64  `json:"sourceDocId"`
}

// Release 释放未核销预占（幂等：无 held 行即成功）。
func Release(db *gorm.DB, req ReleaseReq) error {
	return db.Model(&model.StockReservation{}).
		Where("source_doc_type=? AND source_doc_id=? AND status='held'", req.SourceDocType, req.SourceDocID).
		Updates(map[string]any{"status": model.RvReleased, "released_at": time.Now()}).Error
}
