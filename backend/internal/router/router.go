// Package router 路由与权限点绑定（api-design.md；权限点即菜单表 perm_code）。
package router

import (
	"fmt"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/api"
	"github.com/evolvermon/evolvermon/internal/crud"
	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/jwtx"
)

var dbRef *gorm.DB

// crudRes 注册带权限点的通用 CRUD 资源。
func crudRes[T any](v1 *gin.RouterGroup, db *gorm.DB, perm, path string, opt crud.Options[T]) {
	sub := v1.Group("", middleware.Require(perm))
	opt.Path = path
	crud.Register[T](sub, db, opt)
}

func act(v1 *gin.RouterGroup, perm, method, path string, h gin.HandlerFunc) {
	switch method {
	case "GET":
		v1.GET(path, middleware.Require(perm), h)
	case "POST":
		v1.POST(path, middleware.Require(perm), h)
	case "PUT":
		v1.PUT(path, middleware.Require(perm), h)
	}
}

// ---- 单据创建钩子：自动单号 + 行号/金额回填（总册 7.3） ----

func salesOrderHook(db *gorm.DB, ac *middleware.AuthCtx, so *model.SalesOrder) error {
	docNo, err := data.NextDocNo(db, ac.TenantID, "SO")
	if err != nil {
		return err
	}
	so.DocNo = docNo
	amount := 0.0
	for i := range so.Lines {
		so.Lines[i].LineNo = i + 1
		so.Lines[i].Amount = so.Lines[i].Qty * so.Lines[i].Price
		amount += so.Lines[i].Amount
	}
	so.Amount = amount
	return nil
}

func purchaseOrderHook(db *gorm.DB, ac *middleware.AuthCtx, po *model.PurchaseOrder) error {
	docNo, err := data.NextDocNo(db, ac.TenantID, "PO")
	if err != nil {
		return err
	}
	po.DocNo = docNo
	amount := 0.0
	for i := range po.Lines {
		po.Lines[i].LineNo = i + 1
		po.Lines[i].Amount = po.Lines[i].Qty * po.Lines[i].Price
		amount += po.Lines[i].Amount
	}
	po.Amount = amount
	return nil
}

func purchaseRequestHook(db *gorm.DB, ac *middleware.AuthCtx, pr *model.PurchaseRequest) error {
	docNo, err := data.NextDocNo(db, ac.TenantID, "PR")
	if err != nil {
		return err
	}
	pr.DocNo = docNo
	for i := range pr.Lines {
		pr.Lines[i].LineNo = i + 1
	}
	return nil
}

func workOrderHook(db *gorm.DB, ac *middleware.AuthCtx, wo *model.WorkOrder) error {
	docNo, err := data.NextDocNo(db, ac.TenantID, "WO")
	if err != nil {
		return err
	}
	wo.DocNo = docNo
	return nil
}

func stockOrderHook(db *gorm.DB, ac *middleware.AuthCtx, so *model.StockOrder) error {
	prefix := map[model.StockDocType]string{model.DocRCV: "RCV", model.DocSHP: "SHP", model.DocMV: "MV"}[so.DocType]
	if prefix == "" {
		so.DocType = model.DocSHP
		prefix = "SHP"
	}
	docNo, err := data.NextDocNo(db, ac.TenantID, prefix)
	if err != nil {
		return err
	}
	so.DocNo = docNo
	for i := range so.Lines {
		so.Lines[i].LineNo = i + 1
	}
	return nil
}

func userHook(db *gorm.DB, ac *middleware.AuthCtx, u *model.User) error {
	if u.PasswordHash == "" {
		hash, _ := bcrypt.GenerateFromPassword([]byte("Demo@123"), bcrypt.DefaultCost)
		u.PasswordHash = string(hash)
	}
	return nil
}

// userAfterCreate 新用户默认归属当前工厂（需已生成的用户 ID）。
func userAfterCreate(db *gorm.DB, ac *middleware.AuthCtx, u *model.User) error {
	if ac.FactoryID > 0 {
		db.Create(&model.UserFactory{UserID: u.ID, FactoryID: ac.FactoryID, IsDefault: true})
	}
	return nil
}

func New(db *gorm.DB, jm *jwtx.Manager) *gin.Engine {
	dbRef = db
	r := gin.New()
	r.Use(gin.Recovery(), middleware.Trace(), middleware.CORS("*"))

	// 健康探针（NFR-052）
	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.GET("/readyz", func(c *gin.Context) {
		if err := db.Exec("SELECT 1").Error; err != nil {
			c.JSON(503, gin.H{"status": "db-down"})
			return
		}
		c.JSON(200, gin.H{"status": "ready"})
	})

	authAPI := &api.AuthAPI{DB: db, JM: jm}
	erp := &api.ErpAPI{DB: db}
	mes := &api.MesAPI{DB: db}
	wms := &api.WmsAPI{DB: db}
	mrp := &api.MrpAPI{DB: db}
	mst := &api.MasterAPI{DB: db}
	plt := &api.PlatformAPI{DB: db}
	board := &api.BoardAPI{DB: db}

	r.POST("/api/v1/auth/login", authAPI.Login)

	v1 := r.Group("/api/v1")
	v1.Use(middleware.Auth(jm, db))

	act(v1, "", "POST", "/auth/refresh", authAPI.Refresh)
	act(v1, "", "GET", "/auth/profile", authAPI.Profile)
	act(v1, "", "POST", "/auth/switch-factory", authAPI.SwitchFactory)
	act(v1, "", "PUT", "/auth/password", authAPI.ChangePassword)

	// ---- 平台 ----
	crudRes[model.Factory](v1, db, "plt:factory", "/factories", crud.Options[model.Factory]{
		FactoryScoped: false, // 工厂属于租户而非工厂，全租户可见（供工厂切换）
		SearchCols: []string{"code", "name"}, FilterCols: []string{"code"},
		// FR-PLT-012：存在未完结单据禁止停用
		BeforeUpdate: func(db *gorm.DB, ac *middleware.AuthCtx, obj *model.Factory, old *model.Factory) error {
			if obj.Status == 0 && old.Status == 1 {
				var wo, so int64
				db.Model(&model.WorkOrder{}).Where("tenant_id=? AND factory_id=? AND status IN ('draft','released','running','paused','finished')", ac.TenantID, old.ID).Count(&wo)
				db.Model(&model.StockOrder{}).Where("tenant_id=? AND factory_id=? AND status IN ('draft','doing')", ac.TenantID, old.ID).Count(&so)
				if wo+so > 0 {
					return response.NewBiz(response.ErrBiz, fmt.Sprintf("存在未完结单据（工单 %d / 出入库 %d），禁止停用", wo, so))
				}
			}
			return nil
		},
	})
	crudRes[model.Warehouse](v1, db, "plt:warehouse", "/warehouses", crud.Options[model.Warehouse]{
		FactoryScoped: true, SearchCols: []string{"code", "name"}, FilterCols: []string{"type"},
	})
	crudRes[model.Location](v1, db, "plt:location", "/locations", crud.Options[model.Location]{
		FactoryScoped: true, SearchCols: []string{"code"}, FilterCols: []string{"warehouse_id"},
	})
	crudRes[model.User](v1, db, "plt:user", "/users", crud.Options[model.User]{
		FactoryScoped: false, SearchCols: []string{"username", "real_name"}, FilterCols: []string{"status"},
		BeforeCreate: userHook, AfterCreate: userAfterCreate,
	})
	crudRes[model.Role](v1, db, "plt:role", "/roles", crud.Options[model.Role]{
		FactoryScoped: false, SearchCols: []string{"code", "name"},
		DenyDelete: func(r *model.Role) string {
			if r.BuiltIn {
				return "内置角色不可删除"
			}
			return ""
		},
	})
	act(v1, "plt:perm", "PUT", "/roles/:id/perms", plt.UpdateRolePerms)
	act(v1, "plt:perm", "PUT", "/users/:id/roles", plt.UpdateUserRoles)
	act(v1, "plt:perm", "GET", "/perms", plt.PermList)
	act(v1, "plt:audit", "GET", "/audit-logs", plt.AuditLogs)
	act(v1, "plt:param", "GET", "/params", plt.GetParams)
	act(v1, "plt:param", "POST", "/params", plt.PutParam)
	// 补齐（PRD 审计 A1~A3）：租户管理 / 单号规则 / 重置密码
	act(v1, "plt:perm", "GET", "/tenants", plt.ListTenants)
	act(v1, "plt:perm", "POST", "/tenants", plt.CreateTenant)
	act(v1, "plt:perm", "PUT", "/tenants/:id", plt.UpdateTenant)
	act(v1, "plt:param", "GET", "/doc-rules", plt.ListDocRules)
	act(v1, "plt:param", "PUT", "/doc-rules/:id", plt.UpdateDocRule)
	act(v1, "plt:user", "PUT", "/users/:id/reset-password", plt.ResetPassword)

	// ---- 主数据 ----
	crudRes[model.Material](v1, db, "erp:material", "/materials", crud.Options[model.Material]{
		FactoryScoped: false, SearchCols: []string{"code", "name", "spec"}, FilterCols: []string{"category", "source_type"},
		// FR-ERP-004：存在未完结单据禁止停用
		BeforeUpdate: func(db *gorm.DB, ac *middleware.AuthCtx, obj *model.Material, old *model.Material) error {
			if obj.Status == 0 && old.Status == 1 {
				var so, po, wo int64
				db.Raw(`SELECT COUNT(*) FROM sales_order_lines l JOIN sales_orders o ON o.id=l.sales_order_id
					WHERE l.material_id=? AND o.status IN ('draft','audited','partial') AND o.deleted_at IS NULL`, old.ID).Scan(&so)
				db.Raw(`SELECT COUNT(*) FROM purchase_order_lines l JOIN purchase_orders o ON o.id=l.purchase_order_id
					WHERE l.material_id=? AND o.status IN ('draft','arriving','receiving') AND o.deleted_at IS NULL`, old.ID).Scan(&po)
				db.Model(&model.WorkOrder{}).Where("tenant_id=? AND material_id=? AND status IN ('draft','released','running','paused')", ac.TenantID, old.ID).Count(&wo)
				if so+po+wo > 0 {
					return response.NewBiz(response.ErrBiz, fmt.Sprintf("物料存在未完结单据（销售 %d / 采购 %d / 工单 %d），禁止停用", so, po, wo))
				}
			}
			return nil
		},
	})
	act(v1, "erp:material", "GET", "/materials/:id/where-used", mst.WhereUsed)
	crudRes[model.Bom](v1, db, "erp:bom", "/boms", crud.Options[model.Bom]{
		FactoryScoped: false, SearchCols: []string{"version_no"}, FilterCols: []string{"material_id", "status"},
		Preload:       []string{"Lines"},
	})
	act(v1, "erp:bom", "POST", "/boms-validate", mst.BomValidate)
	act(v1, "erp:bom", "POST", "/boms/:id/enable", mst.BomEnable)
	act(v1, "erp:bom", "GET", "/boms/:id/tree", mst.BomTree)
	crudRes[model.Routing](v1, db, "erp:routing", "/routings", crud.Options[model.Routing]{
		FactoryScoped: false, SearchCols: []string{"version_no"}, FilterCols: []string{"material_id", "status"},
		Preload:       []string{"Ops"},
	})
	crudRes[model.WorkCenter](v1, db, "erp:workcenter", "/work-centers", crud.Options[model.WorkCenter]{
		FactoryScoped: true, SearchCols: []string{"code", "name"}, FilterCols: []string{"type"},
	})
	crudRes[model.Customer](v1, db, "erp:customer", "/customers", crud.Options[model.Customer]{
		FactoryScoped: false, SearchCols: []string{"code", "name"},
	})
	crudRes[model.Supplier](v1, db, "erp:supplier", "/suppliers", crud.Options[model.Supplier]{
		FactoryScoped: false, SearchCols: []string{"code", "name"},
	})

	// ---- ERP 交易 ----
	crudRes[model.SalesOrder](v1, db, "erp:sales", "/sales-orders", crud.Options[model.SalesOrder]{
		FactoryScoped: true, SearchCols: []string{"doc_no"}, FilterCols: []string{"status", "customer_id"},
		Preload:       []string{"Lines"},
		BeforeCreate:  salesOrderHook,
		DenyDelete: func(so *model.SalesOrder) string {
			if so.Status != model.StDraft {
				return "非草稿单据不可删除，请作废"
			}
			return ""
		},
	})
	act(v1, "erp:sales", "POST", "/sales-orders/:id/audit", erp.AuditSalesOrder)
	act(v1, "erp:sales", "POST", "/sales-orders/:id/void", erp.VoidSalesOrder)
	act(v1, "erp:sales", "POST", "/sales-orders/:id/create-shipment", erp.CreateShipment)

	crudRes[model.PurchaseRequest](v1, db, "erp:purchase", "/purchase-requests", crud.Options[model.PurchaseRequest]{
		FactoryScoped: true, SearchCols: []string{"doc_no"}, FilterCols: []string{"status"},
		Preload:       []string{"Lines"},
		BeforeCreate:  purchaseRequestHook,
	})
	act(v1, "erp:purchase", "POST", "/purchase-requests/convert-to-po", erp.ConvertPRToPO)
	crudRes[model.PurchaseOrder](v1, db, "erp:purchase", "/purchase-orders", crud.Options[model.PurchaseOrder]{
		FactoryScoped: true, SearchCols: []string{"doc_no"}, FilterCols: []string{"status", "supplier_id"},
		Preload:       []string{"Lines"},
		BeforeCreate:  purchaseOrderHook,
	})
	act(v1, "erp:purchase", "POST", "/purchase-orders/:id/audit", erp.AuditPO)
	act(v1, "erp:purchase", "POST", "/purchase-orders/:id/arrive", erp.ArrivePO)

	act(v1, "erp:mrp", "POST", "/mrp/run", mrp.Run)
	act(v1, "erp:mrp", "POST", "/mrp/suggestions/convert", mrp.Convert)

	// ---- MES ----
	crudRes[model.WorkOrder](v1, db, "mes:workorder", "/work-orders", crud.Options[model.WorkOrder]{
		FactoryScoped: true, SearchCols: []string{"doc_no"}, FilterCols: []string{"status", "material_id"},
		Preload:       []string{"Ops"},
		BeforeCreate:  workOrderHook,
	})
	act(v1, "mes:workorder", "POST", "/work-orders/:id/release", mes.Release)
	act(v1, "mes:workorder", "POST", "/work-orders/:id/pause", mes.Pause)
	act(v1, "mes:workorder", "POST", "/work-orders/:id/resume", mes.Resume)
	act(v1, "mes:workorder", "POST", "/work-orders/:id/void", mes.Void)
	act(v1, "mes:workorder", "POST", "/work-orders/:id/close", mes.Close)
	act(v1, "mes:workorder", "POST", "/work-orders/:id/force-finish", mes.ForceFinish)
	act(v1, "mes:workorder", "POST", "/work-orders/:id/picking-request", mes.PickingRequest)
	act(v1, "mes:workorder", "POST", "/work-orders/:id/return-material", mes.ReturnMaterial)
	act(v1, "mes:workorder", "POST", "/work-orders/:id/finish-receipt", mes.FinishReceipt)
	act(v1, "mes:operation", "GET", "/wo-operations", mes.ListOps)
	act(v1, "mes:operation", "POST", "/wo-operations/:id/assign", mes.AssignOp)
	act(v1, "mes:operation", "POST", "/wo-operations/:id/start", mes.StartOp)
	act(v1, "mes:report", "POST", "/production-reports", mes.Report)
	act(v1, "mes:report", "GET", "/production-reports", mes.ListReports)
	crudRes[model.DefectCode](v1, db, "mes:quality", "/defect-codes", crud.Options[model.DefectCode]{
		FactoryScoped: false, SearchCols: []string{"code", "name"}, FilterCols: []string{"category"},
	})
	crudRes[model.QcScheme](v1, db, "mes:quality", "/qc-schemes", crud.Options[model.QcScheme]{
		FactoryScoped: false, FilterCols: []string{"material_id", "check_type"},
	})
	act(v1, "mes:quality", "POST", "/qc-records", mes.CreateQc)
	act(v1, "mes:quality", "GET", "/qc-records", wms.QcRecords)
	act(v1, "mes:quality", "POST", "/qc-records/:id/audit", mes.AuditQc)
	// M4：处置 + 成本 + 财务凭证
	quality := &api.QualityAPI{DB: db}
	finance := &api.FinanceAPI{DB: db}
	act(v1, "mes:quality", "POST", "/qc-records/:id/dispose", quality.Dispose)
	act(v1, "mes:workorder", "POST", "/work-orders/:id/costing", quality.Costing)
	act(v1, "erp:sales", "GET", "/finance/vouchers", finance.Vouchers)
	// M5：厂间调拨 + 集团视图
	transfer := &api.TransferAPI{DB: db}
	act(v1, "wms:stockorder", "POST", "/transfers", transfer.Create)
	act(v1, "plt:perm", "GET", "/group/summary", transfer.GroupSummary)
	// M6：事件出口 + 设备遥测 + 智能建议
	ext := &api.ExtAPI{DB: db}
	act(v1, "plt:perm", "GET", "/webhooks", ext.ListWebhooks)
	act(v1, "plt:perm", "POST", "/webhooks", ext.CreateWebhook)
	act(v1, "plt:perm", "DELETE", "/webhooks/:id", ext.DeleteWebhook)
	act(v1, "plt:perm", "GET", "/webhook-deliveries", ext.ListDeliveries)
	act(v1, "mes:report", "POST", "/devices/:code/telemetry", ext.PushTelemetry)
	act(v1, "mes:report", "GET", "/devices/telemetry/latest", ext.LatestTelemetry)
	act(v1, "mes:workorder", "GET", "/advisor/insights", ext.Insights)
	// 附件（总册 7.3.1）：登录即可，按租户隔离
	attach := &api.AttachmentAPI{DB: db}
	act(v1, "", "POST", "/attachments", attach.Upload)
	act(v1, "", "GET", "/attachments", attach.List)
	act(v1, "", "GET", "/attachments/:id/download", attach.Download)
	act(v1, "mes:quality", "GET", "/trace/work-order/:id", mst.TraceWorkOrder)
	// M3：生产计划域
	plan := &api.PlanAPI{DB: db}
	act(v1, "erp:mrp", "GET", "/atp", plan.Atp)
	act(v1, "mes:workorder", "POST", "/plan/schedule", plan.Schedule)
	act(v1, "mes:workorder", "GET", "/plan/gantt", plan.Gantt)
	act(v1, "mes:operation", "GET", "/plan/dispatch-board", plan.DispatchBoard)
	// M2：序列号/批次追溯 + 单据打印
	traceAPI := &api.TraceAPI{DB: db}
	printAPI := &api.PrintAPI{DB: db}
	act(v1, "mes:quality", "GET", "/trace/serial/:sn", traceAPI.Serial)
	act(v1, "wms:stock", "GET", "/trace/batch/:no", traceAPI.Batch)
	act(v1, "wms:stockorder", "GET", "/print/stock-orders/:id", printAPI.StockOrder)
	act(v1, "mes:workorder", "GET", "/print/work-orders/:id", printAPI.WorkOrder)

	// ---- 看板/工作台 ----
	act(v1, "mes:workorder", "GET", "/kanban/board", board.KanbanBoard)
	act(v1, "mes:report", "GET", "/kanban/today", board.KanbanToday)
	act(v1, "mes:workorder", "GET", "/dashboard/summary", board.DashboardSummary)

	// ---- WMS ----
	act(v1, "wms:stock", "GET", "/stocks", wms.Stocks)
	act(v1, "wms:stock", "GET", "/stocks/available", wms.Available)
	act(v1, "wms:stock", "GET", "/stock-movements", wms.Movements)
	act(v1, "wms:stock", "GET", "/reservations", wms.Reservations)
	act(v1, "wms:stock", "POST", "/reservations/:id/release", wms.ReleaseReservation)
	crudRes[model.StockOrder](v1, db, "wms:stockorder", "/stock-orders", crud.Options[model.StockOrder]{
		FactoryScoped: true, SearchCols: []string{"doc_no"}, FilterCols: []string{"doc_type", "status", "source_type"},
		Preload:       []string{"Lines"},
		BeforeCreate:  stockOrderHook,
	})
	act(v1, "wms:stockorder", "POST", "/stock-orders/:id/post-lines", wms.PostLines)
	act(v1, "wms:stockorder", "POST", "/stocktakings", wms.CreateStocktaking)
	act(v1, "wms:stockorder", "GET", "/stocktakings", wms.ListStocktakings)
	act(v1, "wms:stockorder", "GET", "/stocktakings/:id", wms.GetStocktaking)
	act(v1, "wms:stockorder", "POST", "/stocktakings/:id/start", wms.StartStocktaking)
	act(v1, "wms:stockorder", "POST", "/stocktakings/:id/lines/:lineId", wms.CountLine)
	act(v1, "wms:stockorder", "POST", "/stocktakings/:id/finish", wms.FinishStocktaking)

	// 托管前端构建产物（存在时）：单二进制/单容器部署形态（NFR-020）
	webDir := os.Getenv("EM_WEB_DIR")
	if webDir == "" {
		webDir = "../frontend/dist"
	}
	if st, err := os.Stat(webDir); err == nil && st.IsDir() {
		index := webDir + "/index.html"
		r.Static("/assets", webDir+"/assets")
		r.StaticFile("/favicon.ico", webDir+"/favicon.ico")
		r.NoRoute(func(c *gin.Context) {
			if strings.HasPrefix(c.Request.URL.Path, "/api") {
				c.JSON(404, gin.H{"code": 404, "message": "not found"})
				return
			}
			p := webDir + c.Request.URL.Path
			if _, err := os.Stat(p); err == nil {
				c.File(p)
				return
			}
			c.File(index) // SPA 路由回退
		})
	}

	return r
}
