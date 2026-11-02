// Package seed 幂等种子：菜单、角色、超管、演示租户/双工厂/主数据（DoD：make demo-seed 跑通故事 1–13）。
package seed

import (
	"log"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/model"
)

type menuSeed struct {
	id, parent int64
	name, path, icon string
	sort int
	typ model.MenuType
	perm string
}

func menus() []menuSeed {
	return []menuSeed{
		{1, 0, "工作台", "/dashboard", "DashboardOutlined", 1, model.MenuPage, ""},
		{10, 0, "系统管理", "/system", "SettingOutlined", 90, model.MenuDir, ""},
		{11, 10, "工厂管理", "/system/factories", "", 1, model.MenuPage, "plt:factory"},
		{12, 10, "仓库管理", "/system/warehouses", "", 2, model.MenuPage, "plt:warehouse"},
		{13, 10, "用户管理", "/system/users", "", 3, model.MenuPage, "plt:user"},
		{14, 10, "角色权限", "/system/roles", "", 4, model.MenuPage, "plt:role"},
		{15, 10, "审计日志", "/system/audit-logs", "", 5, model.MenuPage, "plt:audit"},
		{19, 10, "系统运维", "/system/ops", "", 9, model.MenuPage, "ops:service"},
		{16, 10, "系统参数", "/system/params", "", 6, model.MenuPage, "plt:param"},
		{17, 10, "集团视图", "/system/group", "", 7, model.MenuPage, "plt:perm"},
		{18, 10, "Webhook 事件", "/system/webhooks", "", 8, model.MenuPage, "plt:perm"},

		{20, 0, "主数据", "/master", "DatabaseOutlined", 10, model.MenuDir, ""},
		{21, 20, "物料管理", "/master/materials", "", 1, model.MenuPage, "erp:material"},
		{22, 20, "BOM 管理", "/master/boms", "", 2, model.MenuPage, "erp:bom"},
		{23, 20, "工艺路线", "/master/routings", "", 3, model.MenuPage, "erp:routing"},
		{24, 20, "工作中心", "/master/work-centers", "", 4, model.MenuPage, "erp:workcenter"},
		{25, 20, "客户管理", "/master/customers", "", 5, model.MenuPage, "erp:customer"},
		{26, 20, "供应商管理", "/master/suppliers", "", 6, model.MenuPage, "erp:supplier"},

		{30, 0, "销售采购", "/erp", "ShopOutlined", 20, model.MenuDir, ""},
		{31, 30, "销售订单", "/erp/sales-orders", "", 1, model.MenuPage, "erp:sales"},
		{32, 30, "请购单", "/erp/purchase-requests", "", 2, model.MenuPage, "erp:purchase"},
		{33, 30, "采购订单", "/erp/purchase-orders", "", 3, model.MenuPage, "erp:purchase"},
		{34, 30, "MRP 运算", "/erp/mrp", "", 4, model.MenuPage, "erp:mrp"},
		{35, 30, "ATP 交期承诺", "/erp/atp", "", 5, model.MenuPage, "erp:mrp"},
		{36, 30, "财务凭证", "/erp/finance", "", 6, model.MenuPage, "erp:sales"},

		{40, 0, "生产制造", "/mes", "ToolOutlined", 30, model.MenuDir, ""},
		{41, 40, "工单管理", "/mes/work-orders", "", 1, model.MenuPage, "mes:workorder"},
		{42, 40, "车间看板", "/mes/kanban", "", 2, model.MenuPage, "mes:workorder"},
		{46, 40, "产能排产", "/mes/scheduling", "", 3, model.MenuPage, "mes:workorder"},
		{47, 40, "大屏看板", "/mes/board", "", 4, model.MenuPage, "mes:workorder"},
		{43, 40, "质检管理", "/mes/quality", "", 5, model.MenuPage, "mes:quality"},
		{44, 40, "不良代码", "/mes/defect-codes", "", 4, model.MenuPage, "mes:quality"},
		{45, 40, "PDA 报工", "/pda/report", "", 5, model.MenuPage, "mes:report"},

		{50, 0, "仓储管理", "/wms", "ContainerOutlined", 40, model.MenuDir, ""},
		{51, 50, "库存台账", "/wms/stocks", "", 1, model.MenuPage, "wms:stock"},
		{52, 50, "出入库单", "/wms/stock-orders", "", 2, model.MenuPage, "wms:stockorder"},
		{53, 50, "库存流水", "/wms/movements", "", 3, model.MenuPage, "wms:stock"},
		{54, 50, "盘点管理", "/wms/stocktakings", "", 4, model.MenuPage, "wms:stockorder"},
		{55, 50, "库存预占", "/wms/reservations", "", 5, model.MenuPage, "wms:stock"},
		{56, 50, "厂间调拨", "/wms/transfers", "", 6, model.MenuPage, "wms:stockorder"},
	}
}

// RunPlatform identity 库种子（分库形态）：菜单/租户/超管/角色/工厂/演示用户/单号规则/参数（幂等）。
func RunPlatform(db *gorm.DB) error { return runPlatform(db) }

// RunCoreDemo core 库演示数据种子（分库形态）：master/mes 表（假定演示租户/工厂已在 identity 库建立，ID=1/2）。
func RunCoreDemo(db *gorm.DB) error { return seedCorePart(db, 1) }

// RunMasterDemo master 库种子（分库形态）：仓库 + 物料/BOM/工艺/工作中心/客商。
func RunMasterDemo(db *gorm.DB) error {
	if err := seedWarehouses(db, 1); err != nil {
		return err
	}
	seedMasterDemo(db, 1)
	return nil
}

// RunCoreSplit 分库形态 core 库种子（不建 master 表）：IQC 方案/期初库存/不良字典。
// byCode=master 库自增固定映射（FG1/SF2/RM-3001..3004=3..6），DG-RAW=1。
func RunCoreSplit(db *gorm.DB) error {
	byCode := map[string]int64{"FG-1000": 1, "SF-2000": 2, "RM-3001": 3, "RM-3002": 4, "RM-3003": 5, "RM-3004": 6}
	seedCoreStock(db, 1, byCode, 1)
	seedDict(db, 1)
	return nil
}

func Run(db *gorm.DB) error { // Lite：platform + core 演示数据
	if err := runPlatform(db); err != nil {
		return err
	}
	return seedCorePart(db, 1)
}

func runPlatform(db *gorm.DB) error {
	// 菜单 upsert
	for _, m := range menus() {
		var cnt int64
		db.Model(&model.Menu{}).Where("id=?", m.id).Count(&cnt)
		if cnt == 0 {
			db.Create(&model.Menu{
				ID: m.id, ParentID: m.parent, Name: m.name, Path: m.path, Icon: m.icon,
				Sort: m.sort, Type: m.typ, PermCode: m.perm, Visible: true,
			})
		}
	}

	// 超管租户
	var tenant model.Tenant
	if err := db.Where("code='EVOLVER'").First(&tenant).Error; err != nil {
		tenant = model.Tenant{Code: "EVOLVER", Name: "Evolver 示例集团", ContactName: "管理员", ContactPhone: "13800000000", Status: 1}
		db.Create(&tenant)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("Admin@123"), bcrypt.DefaultCost)

	var admin model.User
	if err := db.Where("username='admin'").First(&admin).Error; err != nil {
		admin = model.User{
			Username: "admin", PasswordHash: string(hash), RealName: "超级管理员",
			IsSuper: true, Status: 1,
		}
		admin.TenantID = tenant.ID
		db.Create(&admin)
	}

	if err := seedRoles(db, tenant.ID); err != nil {
		return err
	}
	if err := seedPlatformPart(db, tenant.ID); err != nil {
		return err
	}
	// 超管默认挂到第一家工厂，保证登录后有工厂上下文（否则 factory_id=0 造成数据错位）
	var adminRef model.User
	if err := db.Where("username='admin' AND tenant_id=?", tenant.ID).First(&adminRef).Error; err == nil {
		if db.Where("user_id=?", adminRef.ID).First(&model.UserFactory{}).Error != nil {
			if fac := firstFactory(db, tenant.ID); fac != nil {
				db.Create(&model.UserFactory{UserID: adminRef.ID, FactoryID: fac.ID, IsDefault: true})
			}
		}
	}
	return nil
}

func firstFactory(db *gorm.DB, tenantID int64) *model.Factory {
	var f model.Factory
	if err := db.Where("tenant_id=? AND status=1", tenantID).Order("id").First(&f).Error; err != nil {
		return nil
	}
	return &f
}

func seedRoles(db *gorm.DB, tenantID int64) error {
	roleDefs := []struct {
		code, name string
		scope      model.DataScope
		perms      string
	}{
		{"tenant_admin", "租户管理员", model.ScopeTenant, "plt:factory,plt:warehouse,plt:location,plt:user,plt:role,plt:perm,plt:audit,plt:param,erp:material,erp:bom,erp:routing,erp:workcenter,erp:customer,erp:supplier,erp:sales,erp:purchase,erp:mrp,mes:workorder,mes:operation,mes:report,mes:quality,wms:stock,wms:stockorder"},
		{"planner", "计划员", model.ScopeFactory, "erp:material,erp:bom,erp:routing,erp:workcenter,erp:purchase,erp:mrp,mes:workorder,mes:operation,wms:stock"},
		{"purchaser", "采购员", model.ScopeFactory, "erp:material,erp:supplier,erp:purchase,wms:stock"},
		{"salesman", "销售员", model.ScopeFactory, "erp:material,erp:customer,erp:sales,wms:stock"},
		{"workshop_supervisor", "车间主管", model.ScopeFactory, "mes:workorder,mes:operation,mes:report,mes:quality,erp:material"},
		{"operator", "操作工", model.ScopeSelf, "mes:report"},
		{"qc_inspector", "质检员", model.ScopeFactory, "mes:quality,erp:material"},
		{"warehouse_keeper", "仓管员", model.ScopeFactory, "wms:stock,wms:stockorder,erp:material"},
	}
	for _, r := range roleDefs {
		var cnt int64
		db.Model(&model.Role{}).Where("tenant_id=? AND code=?", tenantID, r.code).Count(&cnt)
		if cnt == 0 {
			db.Create(&model.Role{
				TenantID: tenantID, Code: r.code, Name: r.name, DataScope: r.scope,
				BuiltIn: true, PermCodes: r.perms, Status: 1,
			})
		}
	}
	return nil
}

// seedPlatformPart platform 表种子（identity 库）：工厂/演示用户/单号规则/参数。
func seedPlatformPart(db *gorm.DB, tenantID int64) error {
	var cnt int64
	db.Model(&model.Factory{}).Where("tenant_id=?", tenantID).Count(&cnt)
	if cnt > 0 {
		return nil
	}
	dg := model.Factory{Code: "DG", Name: "东莞示范厂", Address: "广东省东莞市长安镇", ManagerUser: "陈厂长", Status: 1}
	sz := model.Factory{Code: "SZ", Name: "苏州示范厂", Address: "江苏省苏州市工业园区", ManagerUser: "刘厂长", Status: 1}
	dg.TenantID, sz.TenantID = tenantID, tenantID
	if err := db.Create(&dg).Error; err != nil {
		return err
	}
	db.Create(&sz)


	// 演示用户
	hash, _ := bcrypt.GenerateFromPassword([]byte("Demo@123"), bcrypt.DefaultCost)
	users := []struct {
		username, real, role string
	}{
		{"planner", "PMC 小李", "planner"},
		{"keeper", "仓管阿芳", "warehouse_keeper"},
		{"supervisor", "车间老王", "workshop_supervisor"},
		{"qc", "质检小周", "qc_inspector"},
		{"sales", "销售小吴", "salesman"},
	}
	for _, u := range users {
		var role model.Role
		db.Where("tenant_id=? AND code=?", tenantID, u.role).First(&role)
		user := model.User{Username: u.username, PasswordHash: string(hash), RealName: u.real, Status: 1}
		user.TenantID = tenantID
		user.FactoryID = &dg.ID
		if err := db.Create(&user).Error; err != nil {
			return err
		}
		db.Create(&model.UserRole{UserID: user.ID, RoleID: role.ID})
		db.Create(&model.UserFactory{UserID: user.ID, FactoryID: dg.ID, IsDefault: true})
		db.Create(&model.UserFactory{UserID: user.ID, FactoryID: sz.ID})
	}

	// 单号规则
	for _, dt := range []string{"SO", "PR", "PO", "WO", "RCV", "SHP", "MV", "STK", "QC"} {
		db.Create(&model.DocNumberRule{TenantID: tenantID, DocType: dt, Prefix: dt, SeqLen: 4})
	}
	// 参数
	db.Create(&model.SysParam{TenantID: tenantID, Key: "allow_negative_stock", Value: `"false"`, Remark: "允许负库存"})
	db.Create(&model.SysParam{TenantID: tenantID, Key: "default_timezone", Value: `"Asia/Shanghai"`, Remark: "默认时区"})

	return nil
}

// seedCorePart core 库种子（master/mes 表）：仓库/演示主数据/不良字典。工厂 ID 为逻辑引用（identity 库自增 1/2）。
func seedCorePart(db *gorm.DB, tenantID int64) error {
	if err := seedWarehouses(db, tenantID); err != nil {
		return err
	}
	byCode := seedMasterDemo(db, tenantID)
	seedCoreStock(db, tenantID, byCode, 1)
	seedDict(db, tenantID)
	return nil
}

func seedMasterDemo(db *gorm.DB, tenantID int64) map[string]int64 {
	var cnt int64
	db.Model(&model.Material{}).Where("tenant_id=?", tenantID).Count(&cnt)
	factoryID := int64(1)
	if cnt > 0 {
		return nil // 幂等：已存在（分库形态 byCode 由固定映射兜底）
	}
	mats := []model.Material{
		{Code: "FG-1000", Name: "智能温控器 V2", Spec: "220V/16A", Category: model.MatFinish, Unit: "台", SourceType: model.SrcMake, BatchManaged: true, SerialManaged: true, SafetyStock: 50, Price: 129, LeadTimeDays: 3, Status: 1},
		{Code: "SF-2000", Name: "温控主板组件", Spec: "PCB-A 版", Category: model.MatSemi, Unit: "件", SourceType: model.SrcMake, BatchManaged: false, SafetyStock: 20, Price: 45, LeadTimeDays: 2, Status: 1},
		{Code: "RM-3001", Name: "PCB 空板", Spec: "双层 1.6mm", Category: model.MatRaw, Unit: "片", SourceType: model.SrcBuy, SafetyStock: 200, Price: 8.5, LeadTimeDays: 7, Status: 1},
		{Code: "RM-3002", Name: "继电器 16A", Spec: "欧姆龙 G4A", Category: model.MatRaw, Unit: "个", SourceType: model.SrcBuy, SafetyStock: 300, Price: 4.2, LeadTimeDays: 5, Status: 1},
		{Code: "RM-3003", Name: "ABS 外壳", Spec: "86 型白", Category: model.MatRaw, Unit: "个", SourceType: model.SrcBuy, BatchManaged: true, SafetyStock: 150, Price: 6.8, LeadTimeDays: 10, Status: 1},
		{Code: "RM-3004", Name: "NTC 温度传感器", Spec: "10K 1%", Category: model.MatRaw, Unit: "个", SourceType: model.SrcBuy, SafetyStock: 500, Price: 1.2, LeadTimeDays: 7, Status: 1},
	}
	for i := range mats {
		mats[i].TenantID = tenantID
		db.Create(&mats[i])
	}
	byCode := map[string]int64{}
	for _, m := range mats {
		byCode[m.Code] = m.ID
	}

	// BOM：FG ← SF-2000×1 + 外壳×1；SF ← PCB×1 + 继电器×1 + 传感器×2
	bomSF := model.Bom{MaterialID: byCode["SF-2000"], VersionNo: "A", Status: model.BomActive}
	bomFG := model.Bom{MaterialID: byCode["FG-1000"], VersionNo: "A", Status: model.BomActive}
	bomSF.TenantID, bomFG.TenantID = tenantID, tenantID
	db.Create(&bomSF)
	db.Create(&model.BomLine{BomID: bomSF.ID, LineNo: 1, MaterialID: byCode["RM-3001"], Qty: 1, ScrapRate: 2})
	db.Create(&model.BomLine{BomID: bomSF.ID, LineNo: 2, MaterialID: byCode["RM-3002"], Qty: 1})
	db.Create(&model.BomLine{BomID: bomSF.ID, LineNo: 3, MaterialID: byCode["RM-3004"], Qty: 2, ScrapRate: 1})
	db.Create(&bomFG)
	db.Create(&model.BomLine{BomID: bomFG.ID, LineNo: 1, MaterialID: byCode["SF-2000"], Qty: 1})
	db.Create(&model.BomLine{BomID: bomFG.ID, LineNo: 2, MaterialID: byCode["RM-3003"], Qty: 1})

	// 工作中心 + 工艺
	wcs := []model.WorkCenter{
		{Code: "WC-SMT", Name: "SMT 贴片线", Type: model.WcLine, CapacityPerDay: 2000, CostRate: 120, Status: 1},
		{Code: "WC-ASM", Name: "装配工位", Type: model.WcLabor, CapacityPerDay: 500, CostRate: 35, Status: 1},
		{Code: "WC-TST", Name: "测试工位", Type: model.WcMachine, CapacityPerDay: 800, CostRate: 60, Status: 1},
	}
	for i := range wcs {
		wcs[i].TenantID = tenantID
		wcs[i].FactoryID = &factoryID
		db.Create(&wcs[i])
	}
	rtSF := model.Routing{MaterialID: byCode["SF-2000"], VersionNo: "A", Status: model.BomActive}
	rtSF.TenantID = tenantID
	db.Create(&rtSF)
	db.Create(&model.RoutingOp{RoutingID: rtSF.ID, Seq: 10, Name: "贴片", WorkCenterID: wcs[0].ID, PrepTimeSec: 600, RunTimePerUnitSec: 15})
	db.Create(&model.RoutingOp{RoutingID: rtSF.ID, Seq: 20, Name: "主板测试", WorkCenterID: wcs[2].ID, PrepTimeSec: 300, RunTimePerUnitSec: 10})
	rtFG := model.Routing{MaterialID: byCode["FG-1000"], VersionNo: "A", Status: model.BomActive}
	rtFG.TenantID = tenantID
	db.Create(&rtFG)
	db.Create(&model.RoutingOp{RoutingID: rtFG.ID, Seq: 10, Name: "整机装配", WorkCenterID: wcs[1].ID, PrepTimeSec: 600, RunTimePerUnitSec: 60})
	db.Create(&model.RoutingOp{RoutingID: rtFG.ID, Seq: 20, Name: "整机测试", WorkCenterID: wcs[2].ID, PrepTimeSec: 300, RunTimePerUnitSec: 30})
	db.Create(&model.RoutingOp{RoutingID: rtFG.ID, Seq: 30, Name: "包装", WorkCenterID: wcs[1].ID, PrepTimeSec: 120, RunTimePerUnitSec: 20})


	// 客商
	cust := model.Customer{Code: "C0001", Name: "深圳智联家电", Contact: "张经理", Phone: "13900000001", CreditLimit: 200000, Status: 1}
	supp := model.Supplier{Code: "S0001", Name: "东莞华强电子", Contact: "李经理", Phone: "13700000001", LeadTimeDays: 7, PaymentTerm: "月结 30 天", Status: 1}
	cust.TenantID, supp.TenantID = tenantID, tenantID
	db.Create(&cust)
	db.Create(&supp)

	log.Printf("[seed] demo master data created for tenant %d", tenantID)
	return byCode
}

func seedDict(db *gorm.DB, tenantID int64) {
	defs := []model.DefectCode{
		{Code: "D001", Name: "外观划伤", Category: "外观", Sort: 1, Status: 1},
		{Code: "D002", Name: "尺寸超差", Category: "尺寸", Sort: 2, Status: 1},
		{Code: "D003", Name: "焊点虚焊", Category: "工艺", Sort: 3, Status: 1},
		{Code: "D004", Name: "功能不良", Category: "功能", Sort: 4, Status: 1},
		{Code: "D005", Name: "来料不良", Category: "来料", Sort: 5, Status: 1},
	}
	for _, d := range defs {
		var cnt int64
		db.Model(&model.DefectCode{}).Where("tenant_id=? AND code=?", tenantID, d.Code).Count(&cnt)
		if cnt == 0 {
			d.TenantID = tenantID
			db.Create(&d)
		}
	}
}

// seedCoreStock core 库种子：IQC 方案（mes）+ 期初库存（wms）。
// master 分库后：byCode 为映射（Lite=真实值/master 分库=演示固定值），原料仓固定 DG-RAW=1。
func seedCoreStock(db *gorm.DB, tenantID int64, byCode map[string]int64, factoryID int64) {
	// IQC 检验方案（RM-3001 全检）：触发采购到货「待检量隔离」演示路径
	qcScheme := model.QcScheme{MaterialID: byCode["RM-3001"], CheckType: model.QcIQC, SampleRatio: 1,
		Items: `[{"name":"外观","std":"无划伤","method":"目视"},{"name":"尺寸","std":"1.6±0.1mm","method":"卡尺"}]`}
	qcScheme.TenantID = tenantID
	db.Create(&qcScheme)
	// 初始库存（模拟已有原料库存）
	whRawID := int64(1) // DG-RAW（master 库自增假定）
	initStock := []struct {
		mat  string
		qty  float64
		batch string
	}{
		{"RM-3001", 500, "B20261001"},
		{"RM-3002", 800, ""},
		{"RM-3003", 300, "B20261002"},
		{"RM-3004", 2000, ""},
	}
	for _, s := range initStock {
		st := model.Stock{
			TenantID: tenantID, FactoryID: factoryID, WarehouseID: whRawID,
			MaterialID: byCode[s.mat], BatchNo: s.batch, QtyOnHand: s.qty,
		}
		db.Create(&st)
		db.Create(&model.StockMovement{
			TenantID: tenantID, FactoryID: factoryID, WarehouseID: whRawID, MaterialID: byCode[s.mat],
			BatchNo: s.batch, Direction: model.MoveIn, Qty: s.qty, BizType: "期初导入",
			BeforeQty: 0, AfterQty: s.qty, OpUserID: 0, CreatedAt: time.Now(),
		})
	}
}

// seedWarehouses master 库仓库种子（两厂×4 仓；工厂 ID 为 identity 库逻辑引用 1/2）。
func seedWarehouses(db *gorm.DB, tenantID int64) error {
	dg := model.Factory{ID: 1, Code: "DG"}
	sz := model.Factory{ID: 2, Code: "SZ"}
	for _, f := range []model.Factory{dg, sz} {
		for _, w := range []model.Warehouse{
			{Code: f.Code + "-RAW", Name: f.Name + "·原料仓", Type: model.WhRaw, Status: 1},
			{Code: f.Code + "-FG", Name: f.Name + "·成品仓", Type: model.WhFG, Status: 1},
			{Code: f.Code + "-SEMI", Name: f.Name + "·半成品仓", Type: model.WhSemi, Status: 1},
			{Code: f.Code + "-DEF", Name: f.Name + "·不良品仓", Type: model.WhDefect, Status: 1},
		} {
			w.TenantID, w.FactoryID = tenantID, &f.ID
			db.Create(&w)
		}
	}
	return nil
}

// RunErpDemo erp 库种子（三库形态；演示数据金路径自建，库种子无需表外数据）。
func RunErpDemo(db *gorm.DB) error { return nil }

// RunMesDemo mes 库种子：不良字典。
func RunMesDemo(db *gorm.DB) error { seedDict(db, 1); return nil }

// RunWmsDemo wms 库种子：期初库存（byCode 固定映射，DG-RAW=1）。
func RunWmsDemo(db *gorm.DB) error {
	byCode := map[string]int64{"FG-1000": 1, "SF-2000": 2, "RM-3001": 3, "RM-3002": 4, "RM-3003": 5, "RM-3004": 6}
	seedCoreStock(db, 1, byCode, 1)
	return nil
}
