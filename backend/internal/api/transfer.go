package api

// M5：厂间调拨（一步完成：调出厂扣账 → 在途流水 → 调入厂入账，双单据留痕）+ 集团视图。

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

type TransferAPI struct{ DB *gorm.DB }

type transferReq struct {
	FromFactoryID int64   `json:"fromFactoryId" binding:"required"`
	ToFactoryID   int64   `json:"toFactoryId" binding:"required"`
	MaterialID    int64   `json:"materialId" binding:"required"`
	Qty           float64 `json:"qty" binding:"required,gt=0"`
	BatchNo       string  `json:"batchNo"`
	Remark        string  `json:"remark"`
}

// Create 厂间调拨：原子完成两厂库存增减，生成调出/调入两张已完成单据。
func (t *TransferAPI) Create(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var req transferReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	if req.FromFactoryID == req.ToFactoryID {
		response.Fail(c, response.NewBiz(response.ErrParam, "调入调出厂不能相同"))
		return
	}
	var mat model.Material
	if err := t.DB.Scopes(data.ScopeFor(ac, false)).First(&mat, req.MaterialID).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrBiz, "物料不存在"))
		return
	}
	// 两厂仓库：调出=物料默认可用仓（原料仓兜底）；调入=同类型仓
	whOf := func(factoryID int64, kind string) (*model.Warehouse, error) {
		var wh model.Warehouse
		q := t.DB.Where("tenant_id=? AND factory_id=? AND status=1", ac.TenantID, factoryID)
		if err := q.Where("type=?", kind).First(&wh).Error; err != nil {
			return nil, fmt.Errorf("工厂 %d 未配置 %s 仓", factoryID, kind)
		}
		return &wh, nil
	}
	kind := "raw"
	if mat.Category == model.MatFinish {
		kind = "finished"
	} else if mat.Category == model.MatSemi {
		kind = "semi"
	}
	whOut, err1 := whOf(req.FromFactoryID, kind)
	whIn, err2 := whOf(req.ToFactoryID, kind)
	if err1 != nil || err2 != nil {
		response.Fail(c, response.NewBiz(response.ErrBiz, fmt.Sprintf("%v %v", err1, err2)))
		return
	}

	var outNo, inNo string
	err := t.DB.Transaction(func(tx *gorm.DB) error {
		// 锁定调出厂库存并扣减
		var stock model.Stock
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id=? AND factory_id=? AND warehouse_id=? AND material_id=? AND batch_no=?",
				ac.TenantID, req.FromFactoryID, whOut.ID, req.MaterialID, req.BatchNo).
			First(&stock).Error
		if err == gorm.ErrRecordNotFound {
			return response.NewBiz(response.ErrStockOut, "调出厂无此库存维度（检查批次）")
		}
		if err != nil {
			return err
		}
		if stock.QtyOnHand < req.Qty-1e-9 {
			return response.NewBiz(response.ErrStockOut, "调出厂库存不足（账面 "+f64(stock.QtyOnHand)+"）")
		}
		before := stock.QtyOnHand
		tx.Model(&model.Stock{}).Where("id=?", stock.ID).Update("qty_on_hand", before-req.Qty)

		outNo, _ = data.NextDocNo(tx, ac.TenantID, "SHP")
		outOrder := model.StockOrder{
			DocType: model.DocSHP, WarehouseID: whOut.ID, SourceType: "move",
			SourceDocType: "factory_transfer", Status: model.SkDone,
			PostedBy: ac.UserID, PostedAt: ptrTime(time.Now()), Remark: req.Remark,
		}
		outOrder.TenantID = ac.TenantID
		outOrder.FactoryID = &req.FromFactoryID
		outOrder.DocNo = outNo
		if err := tx.Create(&outOrder).Error; err != nil {
			return err
		}
		tx.Create(&model.StockOrderLine{StockOrderID: outOrder.ID, LineNo: 1,
			MaterialID: req.MaterialID, BatchNo: req.BatchNo, Qty: req.Qty, DoneQty: req.Qty})

		// 调入厂入账
		var inStock model.Stock
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id=? AND factory_id=? AND warehouse_id=? AND material_id=? AND batch_no=?",
				ac.TenantID, req.ToFactoryID, whIn.ID, req.MaterialID, req.BatchNo).
			First(&inStock).Error
		if err == gorm.ErrRecordNotFound {
			inStock = model.Stock{TenantID: ac.TenantID, FactoryID: req.ToFactoryID,
				WarehouseID: whIn.ID, MaterialID: req.MaterialID, BatchNo: req.BatchNo}
			if err := tx.Create(&inStock).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		tx.Model(&model.Stock{}).Where("id=?", inStock.ID).Update("qty_on_hand", inStock.QtyOnHand+req.Qty)

		inNo, _ = data.NextDocNo(tx, ac.TenantID, "RCV")
		inOrder := model.StockOrder{
			DocType: model.DocRCV, WarehouseID: whIn.ID, SourceType: "move",
			SourceDocType: "factory_transfer", Status: model.SkDone,
			PostedBy: ac.UserID, PostedAt: ptrTime(time.Now()), Remark: "厂间调拨来自单 " + outNo,
		}
		inOrder.TenantID = ac.TenantID
		inOrder.FactoryID = &req.ToFactoryID
		inOrder.DocNo = inNo
		if err := tx.Create(&inOrder).Error; err != nil {
			return err
		}
		tx.Create(&model.StockOrderLine{StockOrderID: inOrder.ID, LineNo: 1,
			MaterialID: req.MaterialID, BatchNo: req.BatchNo, Qty: req.Qty, DoneQty: req.Qty})

		// 双流水
		now := time.Now()
		tx.Create(&model.StockMovement{TenantID: ac.TenantID, FactoryID: req.FromFactoryID,
			WarehouseID: whOut.ID, MaterialID: req.MaterialID, BatchNo: req.BatchNo,
			Direction: model.MoveOut, Qty: req.Qty, BizType: "厂间调拨",
			SourceDocType: "stock_order", SourceDocID: outOrder.ID, SourceDocNo: outNo,
			BeforeQty: before, AfterQty: before - req.Qty, OpUserID: ac.UserID, CreatedAt: now})
		tx.Create(&model.StockMovement{TenantID: ac.TenantID, FactoryID: req.ToFactoryID,
			WarehouseID: whIn.ID, MaterialID: req.MaterialID, BatchNo: req.BatchNo,
			Direction: model.MoveIn, Qty: req.Qty, BizType: "厂间调拨",
			SourceDocType: "stock_order", SourceDocID: inOrder.ID, SourceDocNo: inNo,
			BeforeQty: inStock.QtyOnHand, AfterQty: inStock.QtyOnHand + req.Qty, OpUserID: ac.UserID, CreatedAt: now})
		return nil
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	data.Audit(t.DB, ac, c.ClientIP(), "factory-transfer", "stock", "", mat.Code, "",
		fmt.Sprintf("%s:%s qty=%s", outNo, inNo, f64(req.Qty)))
	response.OK2(c, gin.H{"outDocNo": outNo, "inDocNo": inNo})
}

// GroupSummary 集团视图：各厂关键指标聚合（租户级）。
func (t *TransferAPI) GroupSummary(c *gin.Context) {
	ac := middleware.GetAuth(c)
	if !ac.IsSuper && ac.DataScope != model.ScopeTenant {
		response.Fail(c, response.NewBiz(response.ErrForbidden, "仅租户级角色可查看集团视图"))
		return
	}
	var facs []model.Factory
	t.DB.Where("tenant_id=? AND status=1", ac.TenantID).Find(&facs)
	type facRow struct {
		FactoryID   int64   `json:"factoryId"`
		Factory     string  `json:"factory"`
		WoActive    int64   `json:"woActive"`
		WoOverdue   int64   `json:"woOverdue"`
		TodayOk     float64 `json:"todayOk"`
		StockValue  float64 `json:"stockValue"`
		PendingDocs int64   `json:"pendingDocs"`
	}
	rows := []facRow{}
	for _, f := range facs {
		var r facRow
		r.FactoryID, r.Factory = f.ID, f.Name
		t.DB.Model(&model.WorkOrder{}).Where("tenant_id=? AND factory_id=? AND status IN ('released','running','paused')", ac.TenantID, f.ID).Count(&r.WoActive)
		t.DB.Model(&model.WorkOrder{}).Where("tenant_id=? AND factory_id=? AND status IN ('released','running','paused') AND plan_end_at < now()", ac.TenantID, f.ID).Count(&r.WoOverdue)
		t.DB.Raw(`SELECT COALESCE(SUM(qty_ok),0) FROM production_reports WHERE factory_id=? AND reported_at >= current_date`, f.ID).Scan(&r.TodayOk)
		t.DB.Raw(`SELECT COALESCE(SUM(s.qty_on_hand * m.price),0) FROM stocks s JOIN materials m ON m.id=s.material_id WHERE s.factory_id=?`, f.ID).Scan(&r.StockValue)
		t.DB.Model(&model.StockOrder{}).Where("tenant_id=? AND factory_id=? AND status IN ('draft','doing')", ac.TenantID, f.ID).Count(&r.PendingDocs)
		rows = append(rows, r)
	}
	response.OK2(c, rows)
}
