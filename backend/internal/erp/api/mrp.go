package api

// MRP：全量净需求计算（FR-ERP-070~077；算法见 core-flows.md §4，M1 简化版）。

import (
	"evolvermon/backend/internal/client"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"evolvermon/backend/internal/data"
	"evolvermon/backend/internal/middleware"
	"evolvermon/backend/internal/model"
	"evolvermon/backend/internal/pkg/response"
)

type MrpAPI struct {
	DB *gorm.DB
	Master client.MasterData // Phase 2.4：主数据读通道（nil=按环境装配，Lite=直连）
}

type MrpSuggestion struct {
	MaterialID   int64     `json:"materialId"`
	MaterialName string    `json:"materialName"`
	SourceType   string    `json:"sourceType"` // make 生产建议 | buy 采购建议
	NetQty       float64   `json:"netQty"`
	DueDate      time.Time `json:"dueDate"`
	Sources      []string  `json:"sources"` // 需求来源
}

// Run 手动触发 MRP（M1：全量，物料 ≤5000 可接受）。
func (m *MrpAPI) Run(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var req struct {
		MaterialIDs []int64 `json:"materialIds"`
	}
	_ = c.ShouldBindJSON(&req)

	// master 分库：物料经通道（全量或按 ids；Remote 无缓存实读）
	materials, merr := client.M().Materials(c.Request.Context(), ac.TenantID, req.MaterialIDs)
	if merr != nil {
		response.Fail(c, merr)
		return
	}

	// 可用量（现有 − held 预占，下限 0：预占超出现有时不应放大净需求）
	onHand := map[int64]float64{}
	if stockSums, err := client.WMS().StockSums(c.Request.Context(), ac.TenantID, ac.FactoryID); err == nil {
		for _, r := range stockSums {
			onHand[r.MaterialID] = r.Qty
		}
	}
	held := map[int64]float64{}
	if heldSums, err := client.WMS().ReservedSums(c.Request.Context(), ac.TenantID, ac.FactoryID); err == nil {
		for _, r := range heldSums {
			held[r.MaterialID] = r.Qty
		}
	}
	available := func(id int64) float64 { return max(onHand[id]-held[id], 0) }

	// 在途：已审未入库采购
	var pos []model.PurchaseOrder
	m.DB.Scopes(data.ScopeFor(ac, true)).Preload("Lines").
		Where("status IN ('arriving','receiving')").Find(&pos)
	// 在制：未完工工单产出（mes 域经通道；三库拆分后唯一路径）

	supply := map[int64]float64{} // 在途(采购未入库) + 在制(工单未产出)
	for _, po := range pos {
		for _, l := range po.Lines {
			supply[l.MaterialID] += max(l.Qty-l.ReceivedQty, 0)
		}
	}

	// 毛需求：已审核销售订单（未发完部分）
	var sos []model.SalesOrder
	m.DB.Scopes(data.ScopeFor(ac, true)).Preload("Lines").
		Where("status IN ('audited','partial')").Find(&sos)
	type demand struct {
		material int64
		qty      float64
		due      time.Time
		source   string
	}
	demands := map[int64][]demand{}
	for _, so := range sos {
		for _, l := range so.Lines {
			remain := l.Qty - l.ShippedQty
			if remain <= 0 {
				continue
			}
			due := time.Now().AddDate(0, 0, 14)
			if l.DeliveryDate != nil {
				due = *l.DeliveryDate
			} else if so.DeliveryDate != nil {
				due = *so.DeliveryDate
			}
			demands[l.MaterialID] = append(demands[l.MaterialID], demand{l.MaterialID, remain, due, so.DocNo})
		}
	}

	matByID := map[int64]client.MaterialLite{}
	for _, mt := range materials {
		matByID[mt.ID] = mt
	}
	// BOM 索引：启用版本
	type bomRow struct {
		ParentID int64
		ChildID  int64
		Qty      float64
		Scrap    float64
	}
	var bomLines []bomRow
	md := m.Master
	if md == nil {
		md = client.NewMaster(m.DB) // 未注入时按环境装配（Lite=直连）
	}
	be, berr := md.BomEdges(c.Request.Context(), ac.TenantID)
	if berr != nil {
		response.Fail(c, berr)
		return
	}
	bomLines = make([]bomRow, 0, len(be))
	for _, e := range be {
		bomLines = append(bomLines, bomRow{ParentID: e.ParentID, ChildID: e.ChildID, Qty: e.Qty, Scrap: e.Scrap})
	}
	children := map[int64][]bomRow{}
	for _, b := range bomLines {
		children[b.ParentID] = append(children[b.ParentID], b)
	}

	// BFS 按层展开（M1 简化：不做 LLC 预计算，逐层扣减滚动）
	var suggestions []MrpSuggestion
	net := map[int64]float64{} // 本轮已产生的计划供给（参与上层可用计算）

	var expand func(matID int64, qty float64, due time.Time, src string, depth int)
	expand = func(matID int64, qty float64, due time.Time, src string, depth int) {
		if depth > 8 {
			return
		}
		// 净需求 = 毛需求 −（现有 −预占 + 在途在制 + 本轮已计划）
		need := qty - (available(matID) + supply[matID] + net[matID])
		if need < 0 {
			need = 0
		}
		if need > 1e-9 {
			mat, ok := matByID[matID]
			if !ok {
				return
			}
			st := "buy"
			if mat.SourceType == string(model.SrcMake) {
				st = "make"
			}
			suggestions = append(suggestions, MrpSuggestion{
				MaterialID: matID, MaterialName: mat.Name, SourceType: st,
				NetQty: need, DueDate: due.AddDate(0, 0, -mat.LeadTimeDays), Sources: []string{src},
			})
			net[matID] += need
		}
		// 子项需求 = 新计划生产量 × 用量（含损耗）；已有库存/在制的父件不消耗子件
		planned := qty - (available(matID) + supply[matID] + net[matID] - need)
		if planned < 0 {
			planned = 0
		}
		for _, b := range children[matID] {
			childNeed := planned * b.Qty * (1 + b.Scrap/100)
			expand(b.ChildID, childNeed, due, fmt.Sprintf("%s→%s", src, matByID[matID].Code), depth+1)
		}
	}
	for _, ds := range demands {
		for _, d := range ds {
			expand(d.material, d.qty, d.due, d.source, 0)
		}
	}

	response.OK2(c, gin.H{
		"runAt":       time.Now(),
		"salesCount":  len(sos),
		"suggestions": suggestions,
	})
}

// Convert FR-ERP-074：建议一键转工单/请购。
func (m *MrpAPI) Convert(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var req struct {
		Suggestions []MrpSuggestion `json:"suggestions" binding:"required"`
		To          string          `json:"to" binding:"required"` // wo | pr
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	var woNos, prNos []string
	err := m.DB.Transaction(func(tx *gorm.DB) error {
		if req.To == "wo" {
			for _, s := range req.Suggestions {
				if s.SourceType != "make" {
					continue
				}
				res, werr := client.MES().CreateWO(c.Request.Context(), client.WOCreateReq{
					TenantID: ac.TenantID, FactoryID: ac.FactoryID, UserID: ac.UserID,
					MaterialID: s.MaterialID, Qty: s.NetQty, SourceType: "mrp",
					SourceDocNo: joinSources(s.Sources),
					PlanStartAt: time.Now(), PlanEndAt: s.DueDate,
					IdemKey: fmt.Sprintf("mrp:%d:%v:%v", s.MaterialID, s.NetQty, s.DueDate),
				})
				if werr != nil {
					return werr
				}
				woNos = append(woNos, res.DocNo)
			}
			return nil
		}
		docNo, err := data.NextDocNo(tx, ac.TenantID, "PR")
		if err != nil {
			return err
		}
		pr := model.PurchaseRequest{SourceType: "mrp"}
		pr.TenantID = ac.TenantID
		pr.FactoryID = &ac.FactoryID
		pr.DocNo = docNo
		if err := tx.Create(&pr).Error; err != nil {
			return err
		}
		no := 0
		for _, s := range req.Suggestions {
			if s.SourceType != "buy" {
				continue
			}
			no++
			if err := tx.Create(&model.PurchaseRequestLine{
				RequestID: pr.ID, LineNo: no, MaterialID: s.MaterialID, Qty: s.NetQty,
			}).Error; err != nil {
				return err
			}
		}
		prNos = append(prNos, docNo)
		return nil
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	data.Audit(m.DB, ac, c.ClientIP(), "mrp-convert", req.To, "", "", "", fmt.Sprintf("wo:%d pr:%d", len(woNos), len(prNos)))
	response.OK2(c, gin.H{"workOrders": woNos, "purchaseRequests": prNos})
}

func joinSources(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}
