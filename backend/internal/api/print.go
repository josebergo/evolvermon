package api

// 中文单据打印（FR-WMS-060~063 / M2）：输出打印友好 HTML，浏览器 window.print() 落纸。
// 模板内联避免外部资源依赖（NFR-023 离线工厂可用）。

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
)

type PrintAPI struct{ DB *gorm.DB }

func (p *PrintAPI) page(title string, body string) string {
	return `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>` + title + `</title>
<style>
 body{font-family:"SimSun","Microsoft YaHei",serif;margin:24px;color:#000}
 .hd{display:flex;justify-content:space-between;align-items:center;border-bottom:2px solid #000;padding-bottom:8px}
 .hd h1{margin:0;font-size:22px;letter-spacing:4px}
 table{width:100%;border-collapse:collapse;margin-top:12px}
 th,td{border:1px solid #000;padding:5px 8px;font-size:13px;text-align:left}
 th{background:#eee}
 .ft{margin-top:18px;display:flex;justify-content:space-between;font-size:13px}
 .meta{font-size:13px;margin-top:10px;line-height:1.8}
 @media print{ .noprint{display:none} button{display:none} }
</style></head><body>
<div class="noprint"><button onclick="window.print()">打 印</button></div>
` + body + `</body></html>`
}

func (p *PrintAPI) docTitle(o model.StockOrder) string {
	switch {
	case o.DocType == model.DocRCV && o.SourceType == "purchase":
		return "采购入库单"
	case o.DocType == model.DocRCV && o.SourceType == "production":
		return "生产报交入库单"
	case o.DocType == model.DocSHP && o.SourceType == "sales":
		return "送货单"
	case o.DocType == model.DocSHP && o.SourceType == "picking":
		return "领料单"
	case o.DocType == model.DocMV:
		return "移库单"
	}
	return "出入库单"
}

// StockOrder 单据打印。
func (p *PrintAPI) StockOrder(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var o model.StockOrder
	if err := p.DB.Scopes(data.ScopeFor(ac, true)).Preload("Lines").First(&o, c.Param("id")).Error; err != nil {
		c.String(404, "单据不存在")
		return
	}
	var wh model.Warehouse
	p.DB.Scopes(data.ScopeFor(ac, true)).First(&wh, o.WarehouseID)
	var mats []model.Material
	p.DB.Scopes(data.ScopeFor(ac, false)).Find(&mats)
	matName := map[int64]model.Material{}
	for _, m := range mats {
		matName[m.ID] = m
	}
	total := 0.0
	var rows strings.Builder
	for _, l := range o.Lines {
		m := matName[l.MaterialID]
		total += l.Qty
		fmt.Fprintf(&rows, "<tr><td>%d</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td style='text-align:right'>%s</td><td style='text-align:right'>%s</td><td>%s</td></tr>",
			l.LineNo, html.EscapeString(m.Code), html.EscapeString(m.Name), html.EscapeString(m.Spec), html.EscapeString(m.Unit),
			num(l.Qty), num(l.DoneQty), html.EscapeString(l.BatchNo))
	}
	body := `<div class="hd"><h1>` + p.docTitle(o) + `</h1><div>` + html.EscapeString(wh.Name) + `</div></div>
<div class="meta">单号：<b>` + o.DocNo + `</b>　来源：` + html.EscapeString(o.SourceDocNo) + `　日期：` + fmt.Sprint(createdAtOf(o.CreatedAt)) + `</div>
<table><tr><th>行号</th><th>物料编码</th><th>物料名称</th><th>规格</th><th>单位</th><th>数量</th><th>已执行</th><th>批次</th></tr>` + rows.String() + `</table>
<div class="ft"><span>制单：____________</span><span>仓管：____________</span><span>收货/领料人：____________</span><span>客户/主管：____________</span></div>`
	c.Data(200, "text/html; charset=utf-8", []byte(p.page(p.docTitle(o), body)))
}

// WorkOrder 工单 + 工序路由卡（随工单流转，报工签字）。
func (p *PrintAPI) WorkOrder(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var wo model.WorkOrder
	if err := p.DB.Scopes(data.ScopeFor(ac, true)).Preload("Ops").First(&wo, c.Param("id")).Error; err != nil {
		c.String(404, "工单不存在")
		return
	}
	var mat model.Material
	p.DB.Scopes(data.ScopeFor(ac, false)).First(&mat, wo.MaterialID)
	var wcs []model.WorkCenter
	p.DB.Scopes(data.ScopeFor(ac, true)).Find(&wcs)
	wcName := map[int64]string{}
	for _, w := range wcs {
		wcName[w.ID] = w.Name
	}
	var rows strings.Builder
	for _, op := range wo.Ops {
		fmt.Fprintf(&rows, "<tr><td>%d</td><td>%s</td><td>%s</td><td>%s</td><td>%s / %s</td><td style='height:34px'></td><td></td><td></td></tr>",
			op.Seq, html.EscapeString(op.Name), html.EscapeString(wcName[op.WorkCenterID]),
			num(op.PlanHours), num(op.QtyOk), num(op.QtyDefect))
	}
	body := `<div class="hd"><h1>工单路由卡</h1><div>单号：<b>` + wo.DocNo + `</b></div></div>
<div class="meta">产品：` + html.EscapeString(mat.Code+" "+mat.Name) + `（` + html.EscapeString(mat.Spec) + `）　计划量：` + num(wo.Qty) + ` ` + html.EscapeString(mat.Unit) +
		`　合格：` + num(wo.GoodQty) + `　不良：` + num(wo.DefectQty) +
		`<br>计划开工：` + ts(wo.PlanStartAt) + `　计划完工：` + ts(wo.PlanEndAt) + `　状态：` + string(wo.Status) + `</div>
<table><tr><th>序</th><th>工序</th><th>工作中心</th><th>计划工时</th><th>合格/不良</th><th>报工人签字</th><th>检验</th><th>备注</th></tr>` + rows.String() + `</table>
<div class="ft"><span>计划员：____________</span><span>车间主管：____________</span><span>完工确认：____________</span></div>`
	c.Data(200, "text/html; charset=utf-8", []byte(p.page("工单路由卡", body)))
}

func createdAtOf(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

func num(f float64) string {
	s := fmt.Sprintf("%.4f", f)
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ".")
}

func ts(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "—"
	}
	return t.Format("2006-01-02")
}
