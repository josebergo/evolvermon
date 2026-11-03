// Package wapi wms 建单契约与幂等实现（独立于 posting 以避免 client 环依赖）。
// mes/erp 经 CoreWMSWriter 通道建出入库单；
// 幂等键写入 Remark（"idem:<key>"），重复调用返回既有单（reuse=true）。
package wapi

import (
	"fmt"

	"gorm.io/gorm"

	"evolvermon/backend/internal/model"
)

// CreateLine 建单行。
type CreateLine struct {
	MaterialID int64   `json:"materialId"`
	Qty        float64 `json:"qty"`
	Price      float64 `json:"price"`
	BatchNo    string  `json:"batchNo"`
	FromLoc    *int64  `json:"fromLoc"`
	ToLoc      *int64  `json:"toLoc"`
}

// CreateReq 幂等建单请求（ac 由调用方构造：服务间调用携带来源用户上下文）。
type CreateReq struct {
	TenantID     int64  `json:"tenantId"`
	FactoryID    int64  `json:"factoryId"`
	UserID       int64  `json:"userId"`
	DocType      string `json:"docType"` // rcv|shp|mv
	SourceType   string `json:"sourceType"`
	SourceDocType string `json:"sourceDocType"`
	SourceDocID  int64  `json:"sourceDocId"`
	SourceDocNo  string `json:"sourceDocNo"`
	WarehouseID  int64  `json:"warehouseId"`
	Remark       string `json:"remark"`
	IdemKey      string `json:"idemKey"` // 幂等键（调用方保证唯一，如 picking:wo:12:raw）
	Lines        []CreateLine `json:"lines"`
}

// CreateResult 建单结果。
type CreateResult struct {
	OrderID int64  `json:"orderId"`
	DocNo   string `json:"docNo"`
	Reused  bool   `json:"reused"`
}

// DocNo 单号生成钩子（wire 装配=data.NextDocNo；wapi 不直接依赖 data 以避免环）。
var DocNo func(db *gorm.DB, tenantID int64, docType string) (string, error)

// CreateOrder 幂等建单（Local/端点共用核心）。
func CreateOrder(db *gorm.DB, req CreateReq) (*CreateResult, error) {
	idemRemark := ""
	if req.IdemKey != "" {
		idemRemark = "idem:" + req.IdemKey
		var exist model.StockOrder
		if err := db.Where("tenant_id=? AND remark=?", req.TenantID, idemRemark).
			Where("status IN ('draft','doing','done')").
			First(&exist).Error; err == nil {
			return &CreateResult{OrderID: exist.ID, DocNo: exist.DocNo, Reused: true}, nil
		}
	}
	docType := model.StockDocType(req.DocType)
	prefix := map[model.StockDocType]string{model.DocRCV: "RCV", model.DocSHP: "SHP", model.DocMV: "MV"}[docType]
	if prefix == "" {
		return nil, fmt.Errorf("unknown docType %s", req.DocType)
	}
	docNo, err := DocNo(db, req.TenantID, prefix)
	if err != nil {
		return nil, err
	}
	order := model.StockOrder{
		DocType: docType, WarehouseID: req.WarehouseID, SourceType: req.SourceType,
		SourceDocType: req.SourceDocType, SourceDocID: req.SourceDocID, SourceDocNo: req.SourceDocNo,
		Remark: idemRemark,
	}
	order.TenantID = req.TenantID
	order.FactoryID = &req.FactoryID
	order.DocNo = docNo
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		for i, l := range req.Lines {
			line := model.StockOrderLine{
				StockOrderID: order.ID, LineNo: i + 1, MaterialID: l.MaterialID,
				Qty: l.Qty, Price: l.Price, BatchNo: l.BatchNo,
				FromLocationID: l.FromLoc, ToLocationID: l.ToLoc,
			}
			if err := tx.Create(&line).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &CreateResult{OrderID: order.ID, DocNo: order.DocNo}, nil
}

