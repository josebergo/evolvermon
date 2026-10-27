import { create } from 'zustand'

// M6 i18n-lite：antd 组件语言 + 菜单/常用标题双语（完整词条化在 M6+ 扩展）
const dict: Record<string, string> = {
  '工作台': 'Dashboard', '系统管理': 'System', '工厂管理': 'Factories', '仓库管理': 'Warehouses',
  '用户管理': 'Users', '角色权限': 'Roles', '审计日志': 'Audit Logs', '系统参数': 'Parameters',
  '集团视图': 'Group View', 'Webhook 事件': 'Webhooks', '主数据': 'Master Data',
  '物料管理': 'Materials', 'BOM 管理': 'BOM', '工艺路线': 'Routings', '工作中心': 'Work Centers',
  '客户管理': 'Customers', '供应商管理': 'Suppliers', '销售采购': 'ERP', '销售订单': 'Sales Orders',
  '请购单': 'Purchase Requests', '采购订单': 'Purchase Orders', 'MRP 运算': 'MRP',
  'ATP 交期承诺': 'ATP', '财务凭证': 'Vouchers', '生产制造': 'Manufacturing', '工单管理': 'Work Orders',
  '车间看板': 'Shop Floor', '产能排产': 'Scheduling', '大屏看板': 'Big Screen', '质检管理': 'Quality',
  '不良代码': 'Defect Codes', 'PDA 报工': 'PDA Report', '设备遥测': 'Telemetry',
  '仓储管理': 'Warehouse', '库存台账': 'Stock Ledger', '出入库单': 'In/Out Orders',
  '库存流水': 'Movements', '盘点管理': 'Stocktaking', '库存预占': 'Reservations', '厂间调拨': 'Transfers',
}

interface LangState {
  lang: 'zh' | 'en'
  setLang: (l: 'zh' | 'en') => void
}

export const useLang = create<LangState>((set) => ({
  lang: (localStorage.getItem('em_lang') as 'zh' | 'en') || 'zh',
  setLang: (l) => { localStorage.setItem('em_lang', l); set({ lang: l }) },
}))

export function menuLabel(name: string, lang: string): string {
  return lang === 'en' ? dict[name] || name : name
}
