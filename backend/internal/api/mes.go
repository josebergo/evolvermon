package api

// MES：工单生命周期、派工、报工（幂等）、质检（FR-MES-001~053；流程见 core-flows.md §2.2）。

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/events"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

type MesAPI struct{ DB *gorm.DB }

// Release FR-MES-002：下达=固化 BOM/工艺快照+生成工序。
func (m *MesAPI) Release(c *gin.Context) {
	ac := middleware.GetAuth(c)
	wo, ok := m.loadWO(c, ac)
	if !ok {
		return
	}
	if wo.Status != model.WoDraft {
		response.Fail(c, response.NewBiz(response.ErrBadState, "仅草稿可下达"))
		return
	}
	// 取当前启用 BOM 与工艺（无工艺允许下达——手工线场景）
	var bom model.Bom
	var routing model.Routing
	bomJSON, routingJSON := "{}", "{}"
	if wo.BomID != nil {
		if err := m.DB.Scopes(data.ScopeFor(ac, false)).Preload("Lines").First(&bom, *wo.BomID).Error; err != nil {
			response.Fail(c, response.NewBiz(response.ErrBiz, "BOM 不存在"))
			return
		}
		b, _ := json.Marshal(bom)
		bomJSON = string(b)
	} else {
		// 未显式选择时取启用版本
		m.DB.Scopes(data.ScopeFor(ac, false)).Preload("Lines").
			Where("material_id=? AND status='active'", wo.MaterialID).First(&bom)
		if bom.ID != 0 {
			wo.BomID = &bom.ID
			b, _ := json.Marshal(bom)
			bomJSON = string(b)
		}
	}
	if wo.RoutingID != nil {
		if err := m.DB.Scopes(data.ScopeFor(ac, false)).Preload("Ops").First(&routing, *wo.RoutingID).Error; err != nil {
			response.Fail(c, response.NewBiz(response.ErrBiz, "工艺路线不存在"))
			return
		}
		r, _ := json.Marshal(routing)
		routingJSON = string(r)
	} else {
		m.DB.Scopes(data.ScopeFor(ac, false)).Preload("Ops").
			Where("material_id=? AND status='active'", wo.MaterialID).First(&routing)
		if routing.ID != 0 {
			wo.RoutingID = &routing.ID
			r, _ := json.Marshal(routing)
			routingJSON = string(r)
		}
	}
	err := m.DB.Transaction(func(tx *gorm.DB) error {
		for _, op := range routing.Ops {
			if err := tx.Create(&model.WoOperation{
				WorkOrderID: wo.ID, Seq: op.Seq, Name: op.Name, WorkCenterID: op.WorkCenterID,
				PlanHours: float64(op.PrepTimeSec)/3600 + wo.Qty*op.RunTimePerUnitSec/3600,
				Status:    model.OpWaiting,
			}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&model.WorkOrder{}).Where("id=? AND version=?", wo.ID, wo.Version).Updates(map[string]any{
			"status": model.WoReleased, "bom_id": wo.BomID, "bom_snapshot": bomJSON,
			"routing_id": wo.RoutingID, "routing_snapshot": routingJSON, "version": wo.Version + 1,
		}).Error
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	data.Audit(m.DB, ac, c.ClientIP(), "release", "work_order", wo.DocNo, wo.DocNo, "", "")
	events.Publish(m.DB, ac.TenantID, "workorder.released", gin.H{"docNo": wo.DocNo, "qty": wo.Qty, "factoryId": ac.FactoryID})
	response.OK2(c, gin.H{"ok": true, "bomSnapshot": json.RawMessage(bomJSON)})
}

// Pause / Resume / Void / Close / ForceFinish
func (m *MesAPI) transition(c *gin.Context, from []model.WoStatus, to model.WoStatus, extra map[string]any) {
	ac := middleware.GetAuth(c)
	wo, ok := m.loadWO(c, ac)
	if !ok {
		return
	}
	allowed := false
	for _, f := range from {
		if wo.Status == f {
			allowed = true
		}
	}
	if !allowed {
		response.Fail(c, response.NewBiz(response.ErrBadState, "状态 "+string(wo.Status)+" 不允许该操作"))
		return
	}
	updates := map[string]any{"status": to, "version": wo.Version + 1}
	for k, v := range extra {
		updates[k] = v
	}
	if err := m.DB.Model(&model.WorkOrder{}).Where("id=? AND version=?", wo.ID, wo.Version).Updates(updates).Error; err != nil {
		response.Fail(c, err)
		return
	}
	data.Audit(m.DB, ac, c.ClientIP(), string(to), "work_order", wo.DocNo, wo.DocNo, "", "")
	response.OK2(c, gin.H{"ok": true})
}

func (m *MesAPI) Pause(c *gin.Context)  { m.transition(c, []model.WoStatus{model.WoRunning, model.WoReleased}, model.WoPaused, nil) }
func (m *MesAPI) Resume(c *gin.Context) { m.transition(c, []model.WoStatus{model.WoPaused}, model.WoRunning, nil) }
func (m *MesAPI) Void(c *gin.Context) {
	ac := middleware.GetAuth(c)
	wo, ok := m.loadWO(c, ac)
	if !ok {
		return
	}
	if wo.ReportedQty > 0 {
		response.Fail(c, response.NewBiz(response.ErrBadState, "已有报工记录，不可作废（请走强制关闭）"))
		return
	}
	if err := m.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("work_order_id=?", wo.ID).Delete(&model.WoOperation{}).Error; err != nil {
			return err
		}
		return tx.Model(&model.WorkOrder{}).Where("id=? AND version=?", wo.ID, wo.Version).
			Updates(map[string]any{"status": model.WoVoid, "version": wo.Version + 1}).Error
	}); err != nil {
		response.Fail(c, err)
		return
	}
	data.Audit(m.DB, ac, c.ClientIP(), "void", "work_order", wo.DocNo, wo.DocNo, "", "")
	response.OK2(c, gin.H{"ok": true})
}
func (m *MesAPI) Close(c *gin.Context) {
	m.transition(c, []model.WoStatus{model.WoReleased, model.WoRunning, model.WoPaused, model.WoFinished, model.WoStored}, model.WoClosed, nil)
}

// ForceFinish 强制完工（不足量场景）。
func (m *MesAPI) ForceFinish(c *gin.Context) {
	ac := middleware.GetAuth(c)
	wo, ok := m.loadWO(c, ac)
	if !ok {
		return
	}
	if wo.Status != model.WoRunning && wo.Status != model.WoReleased {
		response.Fail(c, response.NewBiz(response.ErrBadState, "当前状态不可完工"))
		return
	}
	now := time.Now()
	err := m.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.WoOperation{}).Where("work_order_id=? AND status IN ('waiting','assigned','running')", wo.ID).
			Updates(map[string]any{"status": model.OpDone, "ended_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&model.WorkOrder{}).Where("id=? AND version=?", wo.ID, wo.Version).
			Updates(map[string]any{"status": model.WoFinished, "actual_end_at": now, "version": wo.Version + 1}).Error
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	data.Audit(m.DB, ac, c.ClientIP(), "force-finish", "work_order", wo.DocNo, wo.DocNo, "", "")
	response.OK2(c, gin.H{"ok": true})
}

// PickingRequest FR-MES-004：按 BOM 快照生成领料出库草稿。
// 原料行与半成品行分属不同仓库类型 → 按仓拆单（原料仓/半成品仓各一张）。
func (m *MesAPI) PickingRequest(c *gin.Context) {
	ac := middleware.GetAuth(c)
	wo, ok := m.loadWO(c, ac)
	if !ok {
		return
	}
	if wo.Status == model.WoDraft || wo.Status == model.WoVoid || wo.Status == model.WoClosed {
		response.Fail(c, response.NewBiz(response.ErrBadState, "工单状态不允许领料"))
		return
	}
	var snapshot model.Bom
	if err := json.Unmarshal([]byte(wo.BomSnapshot), &snapshot); err != nil || len(snapshot.Lines) == 0 {
		response.Fail(c, response.NewBiz(response.ErrBiz, "工单无 BOM 快照，请手工建出库单"))
		return
	}
	// 仓库按类型就位
	var whRaw, whSemi model.Warehouse
	if err := m.DB.Scopes(data.ScopeFor(ac, true)).Where("type='raw' AND status=1").First(&whRaw).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrBiz, "未配置原料仓"))
		return
	}
	m.DB.Scopes(data.ScopeFor(ac, true)).Where("type='semi' AND status=1").First(&whSemi)

	// 行分类：半成品物料 → 半成品仓；其余 → 原料仓
	matIDs := make([]int64, 0, len(snapshot.Lines))
	for _, l := range snapshot.Lines {
		matIDs = append(matIDs, l.MaterialID)
	}
	var mats []model.Material
	m.DB.Scopes(data.ScopeFor(ac, false)).Where("id IN ?", matIDs).Find(&mats)
	isSemi := map[int64]bool{}
	for _, mt := range mats {
		isSemi[mt.ID] = mt.Category == model.MatSemi
	}

	orders := []model.StockOrder{}
	err := m.DB.Transaction(func(tx *gorm.DB) error {
		mk := func(wh model.Warehouse, has bool) error {
			if !has || wh.ID == 0 {
				return nil
			}
			docNo, err := data.NextDocNo(tx, ac.TenantID, "SHP")
			if err != nil {
				return err
			}
			order := model.StockOrder{
				DocType: model.DocSHP, WarehouseID: wh.ID, SourceType: "picking",
				SourceDocType: "work_order", SourceDocID: wo.ID, SourceDocNo: wo.DocNo,
			}
			order.TenantID = ac.TenantID
			order.FactoryID = &ac.FactoryID
			order.DocNo = docNo
			if err := tx.Create(&order).Error; err != nil {
				return err
			}
			no := 0
			for _, l := range snapshot.Lines {
				if isSemi[l.MaterialID] != (wh.Type == model.WhSemi) {
					continue
				}
				no++
				need := wo.Qty * l.Qty * (1 + l.ScrapRate/100)
				if err := tx.Create(&model.StockOrderLine{
					StockOrderID: order.ID, LineNo: no, MaterialID: l.MaterialID, Qty: need,
				}).Error; err != nil {
					return err
				}
			}
			orders = append(orders, order)
			return nil
		}
		if err := mk(whRaw, true); err != nil {
			return err
		}
		return mk(whSemi, whSemi.ID != 0)
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"orders": orders, "docNos": func() []string {
		ns := []string{}
		for _, o := range orders {
			ns = append(ns, o.DocNo)
		}
		return ns
	}()})
}

// FinishReceipt FR-MES-006：完工报交 → 成品入库草稿。
func (m *MesAPI) FinishReceipt(c *gin.Context) {
	ac := middleware.GetAuth(c)
	wo, ok := m.loadWO(c, ac)
	if !ok {
		return
	}
	if wo.Status != model.WoFinished {
		response.Fail(c, response.NewBiz(response.ErrBadState, "仅已完工工单可报交入库"))
		return
	}
	if wo.GoodQty <= 0 {
		response.Fail(c, response.NewBiz(response.ErrBiz, "合格量为 0，不可报交入库（请先报工）"))
		return
	}
	var mat model.Material
	if err := m.DB.Scopes(data.ScopeFor(ac, false)).First(&mat, wo.MaterialID).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrBiz, "物料不存在"))
		return
	}
	// 成品/半成品仓
	whType := "'finished'"
	if mat.Category == model.MatSemi {
		whType = "'semi'"
	}
	var wh model.Warehouse
	if err := m.DB.Scopes(data.ScopeFor(ac, true)).Where("type="+whType+" AND status=1").First(&wh).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrBiz, "未配置对应仓库"))
		return
	}
	batchNo := ""
	if mat.BatchManaged {
		batchNo = "B" + time.Now().Format("060102") + "-" + wo.DocNo[len(wo.DocNo)-4:]
	}
	docNo, _ := data.NextDocNo(m.DB, ac.TenantID, "RCV")
	order := model.StockOrder{
		DocType: model.DocRCV, WarehouseID: wh.ID, SourceType: "production",
		SourceDocType: "work_order", SourceDocID: wo.ID, SourceDocNo: wo.DocNo,
	}
	order.TenantID = ac.TenantID
	order.FactoryID = &ac.FactoryID
	order.DocNo = docNo
	err := m.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.StockOrderLine{
			StockOrderID: order.ID, LineNo: 1, MaterialID: wo.MaterialID, Qty: wo.GoodQty, BatchNo: batchNo,
		}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, order)
}

// ---- 工序 ----

func (m *MesAPI) ListOps(c *gin.Context) {
	ac := middleware.GetAuth(c)
	// WoOperation 无租户列：经 work_orders 校验租户隔离
	q := m.DB.Model(&model.WoOperation{})
	if v := c.Query("work_order_id"); v != "" {
		var wo model.WorkOrder
		if err := m.DB.Scopes(data.ScopeFor(ac, true)).First(&wo, v).Error; err != nil {
			response.Fail(c, response.NewBiz(response.ErrParam, "工单不存在"))
			return
		}
		q = q.Where("work_order_id=?", v)
	} else if c.Query("mine") == "1" {
		q = q.Joins("JOIN work_orders w ON w.id = wo_operations.work_order_id AND w.tenant_id = ? AND w.deleted_at IS NULL", ac.TenantID).
			Where("assignee_user_id=?", ac.UserID)
	} else {
		q = q.Joins("JOIN work_orders w ON w.id = wo_operations.work_order_id AND w.tenant_id = ? AND w.factory_id = ? AND w.deleted_at IS NULL", ac.TenantID, ac.FactoryID)
	}
	var ops []model.WoOperation
	var total int64
	q.Count(&total)
	q.Order("seq").Find(&ops)
	response.Page(c, ops, total)
}

func (m *MesAPI) AssignOp(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var req struct {
		UserID *int64 `json:"userId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	var op model.WoOperation
	// WoOperation 无租户列：先验证所属工单租户
	if err := m.DB.Joins("JOIN work_orders w ON w.id = wo_operations.work_order_id AND w.tenant_id = ?", ac.TenantID).
		First(&op, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "工序不存在"))
		return
	}
	st := model.OpAssigned
	if req.UserID == nil {
		st = model.OpWaiting
	}
	m.DB.Model(&model.WoOperation{}).Where("id=?", op.ID).Updates(map[string]any{"assignee_user_id": req.UserID, "status": st})
	data.Audit(m.DB, ac, c.ClientIP(), "assign", "wo_operation", i64s(op.ID), op.Name, "", "")
	response.OK2(c, gin.H{"ok": true})
}

func (m *MesAPI) StartOp(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var op model.WoOperation
	// WoOperation 无租户列：先验证所属工单租户
	if err := m.DB.Joins("JOIN work_orders w ON w.id = wo_operations.work_order_id AND w.tenant_id = ?", ac.TenantID).
		First(&op, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "工序不存在"))
		return
	}
	if op.Status != model.OpWaiting && op.Status != model.OpAssigned {
		response.Fail(c, response.NewBiz(response.ErrBadState, "工序状态不允许开工"))
		return
	}
	now := time.Now()
	err := m.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.WoOperation{}).Where("id=?", op.ID).Updates(map[string]any{"status": model.OpRunning, "started_at": now}).Error; err != nil {
			return err
		}
		// 工单转执行中
		return tx.Model(&model.WorkOrder{}).Where("id=? AND status IN ('released','paused')", op.WorkOrderID).
			Updates(map[string]any{"status": model.WoRunning, "actual_start_at": now}).Error
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"ok": true})
}

// ---- 报工（高并发核心，幂等） ----

type reportReq struct {
	WorkOrderID int64    `json:"workOrderId" binding:"required"`
	OperationID int64    `json:"operationId" binding:"required"`
	QtyOk       float64  `json:"qtyOk"`
	QtyDefect   float64  `json:"qtyDefect"`
	DefectCodes []string `json:"defectCodes"`
	Hours       float64  `json:"hours"`
	ClientToken string   `json:"clientToken"`
	Remark      string   `json:"remark"`
}

func (m *MesAPI) Report(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var req reportReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	if req.ClientToken == "" {
		req.ClientToken = uuid.NewString()
	}
	if req.QtyOk < 0 || req.QtyDefect < 0 || req.QtyOk+req.QtyDefect == 0 {
		response.Fail(c, response.NewBiz(response.ErrParam, "报工数量必须大于 0"))
		return
	}
	if req.QtyDefect > 0 && len(req.DefectCodes) == 0 {
		response.Fail(c, response.NewBiz(response.ErrParam, "不良数>0 时必须选择不良代码"))
		return
	}
	// 幂等：同 token 已存在直接返回原记录（FR-MES-032）
	var exist model.ProductionReport
	if err := m.DB.Where("client_token=?", req.ClientToken).First(&exist).Error; err == nil {
		response.OK2(c, gin.H{"idempotent": true, "reportId": exist.ID})
		return
	}
	var wo model.WorkOrder
	if err := m.DB.Scopes(data.ScopeFor(ac, true)).First(&wo, req.WorkOrderID).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "工单不存在"))
		return
	}
	if wo.Status != model.WoReleased && wo.Status != model.WoRunning && wo.Status != model.WoPaused {
		response.Fail(c, response.NewBiz(response.ErrBadState, "工单未下达或已完结"))
		return
	}
	var op model.WoOperation
	if err := m.DB.Where("id=? AND work_order_id=?", req.OperationID, req.WorkOrderID).First(&op).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "工序不属于该工单"))
		return
	}
	// 前道工序须已完成（默认顺序强制，PRD OQ-1）
	var prior model.WoOperation
	if err := m.DB.Where("work_order_id=? AND seq<? AND status NOT IN ('done','skipped')", req.WorkOrderID, op.Seq).First(&prior).Error; err == nil {
		response.Fail(c, response.NewBiz(response.ErrBiz, "前道工序未完成："+prior.Name))
		return
	}
	defect := strings.Join(req.DefectCodes, ",")
	rep := model.ProductionReport{
		TenantID: ac.TenantID, FactoryID: &ac.FactoryID,
		WorkOrderID: req.WorkOrderID, WoOperationID: req.OperationID,
		UserID: ac.UserID, Username: ac.Username,
		QtyOk: req.QtyOk, QtyDefect: req.QtyDefect, DefectCodes: defect,
		Hours: req.Hours, ClientToken: req.ClientToken, ReportedAt: time.Now(), Remark: req.Remark,
	}
	err := m.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&rep).Error; err != nil {
			return err
		}
		opNext := model.OpRunning
		if op.Status == model.OpDone {
			opNext = model.OpDone
		}
		woNext := wo.Status
		if wo.Status == model.WoReleased {
			woNext = model.WoRunning
		}
		// 工序与工单累计原子自增（NFR-001 高并发路径：无长事务）；A6：报工即落工序结束时间
		if err := tx.Model(&model.WoOperation{}).Where("id=?", req.OperationID).
			Updates(map[string]any{
				"qty_ok":     gorm.Expr("qty_ok+?", req.QtyOk),
				"qty_defect": gorm.Expr("qty_defect+?", req.QtyDefect),
				"status":     opNext,
				"ended_at":   rep.ReportedAt,
			}).Error; err != nil {
			return err
		}
		return tx.Model(&model.WorkOrder{}).Where("id=?", req.WorkOrderID).
			Updates(map[string]any{
				"reported_qty": gorm.Expr("reported_qty+?", req.QtyOk+req.QtyDefect),
				"good_qty":     gorm.Expr("good_qty+?", req.QtyOk),
				"defect_qty":   gorm.Expr("defect_qty+?", req.QtyDefect),
				"status":       woNext,
			}).Error
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	// 末工序报满自动完工
	var lastOp model.WoOperation
	m.DB.Where("work_order_id=?", req.WorkOrderID).Order("seq DESC").First(&lastOp)
	if lastOp.ID == req.OperationID {
		var w model.WorkOrder
		m.DB.First(&w, req.WorkOrderID)
		if w.GoodQty >= w.Qty && w.Status == model.WoRunning {
			now := time.Now()
			m.DB.Model(&model.WorkOrder{}).Where("id=?", w.ID).
				Updates(map[string]any{"status": model.WoFinished, "actual_end_at": now})
		}
	}
	response.OK2(c, rep)
}

func (m *MesAPI) ListReports(c *gin.Context) {
	ac := middleware.GetAuth(c)
	q := m.DB.Scopes(data.ScopeFor(ac, true)).Model(&model.ProductionReport{})
	if v := c.Query("work_order_id"); v != "" {
		q = q.Where("work_order_id=?", v)
	}
	if c.Query("mine") == "1" {
		q = q.Where("user_id=?", ac.UserID)
	}
	var list []model.ProductionReport
	var total int64
	q.Count(&total)
	q.Order("reported_at DESC").Limit(200).Find(&list)
	response.Page(c, list, total)
}

// ---- 质检 ----

func (m *MesAPI) AuditQc(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var req struct {
		Result    string `json:"result" binding:"required"`
		Disposal  string `json:"disposal"`
		ItemsResult string `json:"itemsResult"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	var rec model.QcRecord
	if err := m.DB.Scopes(data.ScopeFor(ac, true)).First(&rec, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "质检单不存在"))
		return
	}
	if rec.Result != model.QcPending {
		response.Fail(c, response.NewBiz(response.ErrBadState, "质检单已判定"))
		return
	}
	if req.Result == "fail" && req.Disposal == "" {
		response.Fail(c, response.NewBiz(response.ErrParam, "不合格必须选择处置方式"))
		return
	}
	items := req.ItemsResult
	if items == "" {
		items = "{}" // jsonb 不接受空串
	}
	if err := m.DB.Model(&model.QcRecord{}).Where("id=?", rec.ID).Updates(map[string]any{
		"result": req.Result, "disposal": req.Disposal, "items_result": items,
		"inspector_id": ac.UserID, "audited_at": time.Now(),
	}).Error; err != nil {
		response.Fail(c, err)
		return
	}
	// FR-WMS-025 联动：合格或让步放行 → 待检量转可用
	if req.Result == "pass" || req.Result == "concession" {
		m.releasePending(ac, rec, "on_hand", "检验放行转可用")
	}
	data.Audit(m.DB, ac, c.ClientIP(), "qc-audit", "qc_record", rec.DocNo, rec.DocNo, "", req.Result)
	events.Publish(m.DB, ac.TenantID, "qc.audited", gin.H{"docNo": rec.DocNo, "result": req.Result, "disposal": req.Disposal})
	response.OK2(c, gin.H{"ok": true})
}

func (m *MesAPI) CreateQc(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var rec model.QcRecord
	if err := c.ShouldBindJSON(&rec); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	docNo, _ := data.NextDocNo(m.DB, ac.TenantID, "QC")
	rec.TenantID = ac.TenantID
	rec.FactoryID = &ac.FactoryID
	rec.DocNo = docNo
	rec.Result = model.QcPending
	if err := m.DB.Create(&rec).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrBiz, err.Error()))
		return
	}
	response.OK2(c, rec)
}

func (m *MesAPI) loadWO(c *gin.Context, ac *middleware.AuthCtx) (*model.WorkOrder, bool) {
	var wo model.WorkOrder
	if err := m.DB.Scopes(data.ScopeFor(ac, true)).Preload("Ops").First(&wo, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "工单不存在"))
		return nil, false
	}
	return &wo, true
}

// ReturnMaterial A5 退料（FR-MES-005）：按已领净量生成退料入库草稿（默认全额，可改量）。
func (m *MesAPI) ReturnMaterial(c *gin.Context) {
	ac := middleware.GetAuth(c)
	wo, ok := m.loadWO(c, ac)
	if !ok {
		return
	}
	if wo.Status != model.WoReleased && wo.Status != model.WoRunning && wo.Status != model.WoPaused &&
		wo.Status != model.WoFinished && wo.Status != model.WoStored {
		response.Fail(c, response.NewBiz(response.ErrBadState, "工单状态不允许退料"))
		return
	}
	// 已领净量 = 领料过账量 − 已退量（按物料汇总）
	type row struct {
		MaterialID int64
		Picked     float64
	}
	var rows []row
	m.DB.Raw(`SELECT l.material_id, SUM(l.done_qty) AS picked
		FROM stock_order_lines l
		JOIN stock_orders o ON o.id=l.stock_order_id
		WHERE o.tenant_id=? AND o.source_type='picking' AND o.source_doc_id=? AND o.status='done'
		GROUP BY l.material_id`, ac.TenantID, wo.ID).Scan(&rows)
	var rets []row
	m.DB.Raw(`SELECT l.material_id, SUM(l.done_qty) AS picked
		FROM stock_order_lines l
		JOIN stock_orders o ON o.id=l.stock_order_id
		WHERE o.tenant_id=? AND o.source_type='return' AND o.source_doc_id=? AND o.status IN ('done','doing')
		GROUP BY l.material_id`, ac.TenantID, wo.ID).Scan(&rets)
	returned := map[int64]float64{}
	for _, r := range rets {
		returned[r.MaterialID] = r.Picked
	}
	var wh model.Warehouse
	if err := m.DB.Scopes(data.ScopeFor(ac, true)).Where("type='raw' AND status=1").First(&wh).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrBiz, "未配置原料仓"))
		return
	}
	docNo, _ := data.NextDocNo(m.DB, ac.TenantID, "RCV")
	order := model.StockOrder{
		DocType: model.DocRCV, WarehouseID: wh.ID, SourceType: "return",
		SourceDocType: "work_order", SourceDocID: wo.ID, SourceDocNo: wo.DocNo,
		Remark: "工单退料",
	}
	order.TenantID = ac.TenantID
	order.FactoryID = &ac.FactoryID
	order.DocNo = docNo
	// 批次物料退料需回原批次：取该工单最近一次领料出库流水的批次
	lastBatch := map[int64]string{}
	var moves []model.StockMovement
	m.DB.Raw(`SELECT DISTINCT ON (material_id) material_id, batch_no
		FROM stock_movements WHERE tenant_id=? AND biz_type='领料出库'
		AND source_doc_id IN (SELECT id FROM stock_orders WHERE source_type='picking' AND source_doc_id=?)
		ORDER BY material_id, id DESC`, ac.TenantID, wo.ID).Scan(&moves)
	for _, mv := range moves {
		lastBatch[mv.MaterialID] = mv.BatchNo
	}

	no := 0
	err := m.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		for _, r := range rows {
			remain := r.Picked - returned[r.MaterialID]
			if remain <= 1e-9 {
				continue
			}
			no++
			if err := tx.Create(&model.StockOrderLine{
				StockOrderID: order.ID, LineNo: no, MaterialID: r.MaterialID, Qty: remain,
				BatchNo: lastBatch[r.MaterialID],
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	data.Audit(m.DB, ac, c.ClientIP(), "return-material", "work_order", wo.DocNo, wo.DocNo, "", order.DocNo)
	response.OK2(c, order)
}

// releasePending 将质检单对应的待检量转入目标桶（on_hand/defective），无待检量时静默跳过（兼容历史数据）。
func (m *MesAPI) releasePending(ac *middleware.AuthCtx, rec model.QcRecord, to, bizType string) {
	if rec.SourceDocType != "stock_order" || rec.SourceDocID == 0 {
		return
	}
	var order model.StockOrder
	if err := m.DB.First(&order, rec.SourceDocID).Error; err != nil {
		return
	}
	factoryID := ac.FactoryID
	if order.FactoryID != nil {
		factoryID = *order.FactoryID
	}
	var stock model.Stock
	if err := m.DB.Where("tenant_id=? AND factory_id=? AND warehouse_id=? AND material_id=? AND batch_no=?",
		ac.TenantID, factoryID, order.WarehouseID, rec.MaterialID, rec.BatchNo).
		First(&stock).Error; err != nil || stock.QtyPending < rec.Qty-1e-9 {
		return // 无待检量：历史/非隔离路径，跳过
	}
	wms := WmsAPI{DB: m.DB}
	_ = wms.TransferBucket(m.DB, ac, order.WarehouseID, model.StockOrderLine{MaterialID: rec.MaterialID},
		rec.BatchNo, rec.Qty, "pending", to, bizType, rec.DocNo)
}
