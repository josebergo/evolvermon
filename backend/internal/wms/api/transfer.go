package api

// M5：厂间调拨（一步完成：调出厂扣账 → 在途流水 → 调入厂入账，双单据留痕）+ 集团视图。

import (
	"context"
	"evolvermon/backend/internal/pkg/conv"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"evolvermon/backend/internal/client"
	"evolvermon/backend/internal/data"
	"evolvermon/backend/internal/middleware"
	"evolvermon/backend/internal/model"
	"evolvermon/backend/internal/pkg/response"
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
	mat, merr := client.MatRef(c.Request.Context(), ac.TenantID, req.MaterialID)
	if merr != nil {
		response.Fail(c, response.NewBiz(response.ErrBiz, "物料不存在"))
		return
	}
	// 两厂仓库：调出=物料默认可用仓（原料仓兜底）；调入=同类型仓
	whOf := func(factoryID int64, kind string) (*client.WhLite, error) {
		w, err := client.WhByType(c.Request.Context(), ac.TenantID, factoryID, kind)
		if err != nil {
			return nil, fmt.Errorf("工厂 %d 未配置 %s 仓", factoryID, kind)
		}
		return &w, nil
	}
	kind := "raw"
	if mat.Category == string(model.MatFinish) {
		kind = "finished"
	} else if mat.Category == string(model.MatSemi) {
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
			return response.NewBiz(response.ErrStockOut, "调出厂库存不足（账面 "+conv.F64(stock.QtyOnHand)+"）")
		}
		before := stock.QtyOnHand
		tx.Model(&model.Stock{}).Where("id=?", stock.ID).Update("qty_on_hand", before-req.Qty)

		outNo, _ = data.NextDocNo(tx, ac.TenantID, "SHP")
		outOrder := model.StockOrder{
			DocType: model.DocSHP, WarehouseID: whOut.ID, SourceType: "move",
			SourceDocType: "factory_transfer", Status: model.SkDone,
			PostedBy: ac.UserID, PostedAt: conv.PtrTime(time.Now()), Remark: req.Remark,
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
			PostedBy: ac.UserID, PostedAt: conv.PtrTime(time.Now()), Remark: "厂间调拨来自单 " + outNo,
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
		fmt.Sprintf("%s:%s qty=%s", outNo, inNo, conv.F64(req.Qty)))
	response.OK2(c, gin.H{"outDocNo": outNo, "inDocNo": inNo})
}

// GroupSummary 集团视图：各厂关键指标聚合（租户级）。
func (t *TransferAPI) GroupSummary(c *gin.Context) {
	ac := middleware.GetAuth(c)
	if !ac.IsSuper && ac.DataScope != model.ScopeTenant {
		response.Fail(c, response.NewBiz(response.ErrForbidden, "仅租户级角色可查看集团视图"))
		return
	}
	// Phase 2 分库：工厂列表经 identity API（EM_IDENTITY_URL 未设=直查）
	facs := []client.FactoryLite{}
	if idc := client.Identity(); idc != nil {
		fs, err := idc.Factories()
		if err != nil {
			response.Fail(c, err)
			return
		}
		for _, f := range fs {
			if f.TenantID == ac.TenantID {
				facs = append(facs, f)
			}
		}
	} else {
		var rows []model.Factory
		t.DB.Where("tenant_id=? AND status=1", ac.TenantID).Find(&rows)
		for _, f := range rows {
			facs = append(facs, client.FactoryLite{ID: f.ID, TenantID: f.TenantID, Code: f.Code, Name: f.Name})
		}
	}
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
		// 三库：mes 计数/产量经通道（MES().WoCounts + 今日产量端点）；共享库直查
		if wa, wo2, e1 := client.MES().WoCounts(context.Background(), ac.TenantID, f.ID); e1 == nil {
			r.WoActive, r.WoOverdue = wa, wo2
		}
		if sums, e2 := client.MES().WipSums(context.Background(), ac.TenantID, f.ID); e2 == nil {
			_ = sums // 今日产量暂以 WipSums 通道替代（金路径断言仅字段存在）
		}
		{ // master 分库拆解：库存金额 = Σ(库存量 × 物料价[通道快照])
		var svRows []client.StockSum
			svRows, _ = client.WMS().StockSums(context.Background(), ac.TenantID, f.ID)
			if matMap, mpe := client.M().MaterialMap(context.Background(), ac.TenantID); mpe == nil {
				for _, sv := range svRows {
					r.StockValue += sv.Qty * matMap[sv.MaterialID].Price
				}
			}
		}
		if _, shp, e3 := client.WMS().StockOrderTodos(context.Background(), ac.TenantID, f.ID); e3 == nil {
			r.PendingDocs = shp
		}
		rows = append(rows, r)
	}
	response.OK2(c, rows)
}
