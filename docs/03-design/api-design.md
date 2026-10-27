# API 设计规范（REST /api/v1）

## 0. 通用约定

- 认证：`Authorization: Bearer <access_token>`；写接口建议带 `Idempotency-Key: <uuid>`。
- 响应统一包：

```json
{ "code": 0, "message": "ok", "data": { }, "trace_id": "..." }
// 失败：code != 0；HTTP 状态码同步语义（401 未认证 / 403 无权限 / 404 / 409 冲突 / 422 参数错 / 500）
```

- 分页：`?page=1&page_size=20&sort=created_at&order=desc` → `data: {list:[], total: 123}`
- 时间：ISO8601 UTC；金额/数量：字符串或数字（JSON number，NUMERIC(18,4) 精度约定文档化）。
- 权限点：`<模块>:<资源>:<动作>`，路由注册时绑定，RBAC 中间件校验。

## 1. 认证与上下文

```
POST /api/v1/auth/login          {username,password} → {access_token, refresh_token, user, factories[]}
POST /api/v1/auth/refresh        {refresh_token} → 新令牌
POST /api/v1/auth/logout
GET  /api/v1/auth/profile        → 用户+角色+权限点+当前工厂
POST /api/v1/auth/switch-factory {factory_id}
PUT  /api/v1/auth/password       {old,new}
```

## 2. 平台（PLT）

```
# 租户（超管）
GET/POST/PUT/DELETE  /api/v1/tenants, /:id
# 工厂/仓库/货位
CRUD  /api/v1/factories | /api/v1/warehouses | /api/v1/locations?warehouse_id=
# 用户/角色
CRUD  /api/v1/users | /api/v1/roles ;  PUT /roles/:id/perms ; PUT /users/:id/roles
# 菜单/参数/单号规则
GET   /api/v1/menus/tree        # 按当前用户权限过滤
GET/PUT /api/v1/params          # 租户参数
GET/POST /api/v1/doc-rules
# 审计
GET   /api/v1/audit-logs?page=…
```

## 3. 主数据（ERP-Master）

```
CRUD /api/v1/materials, /:id ; GET /materials/:id/where-used
GET/POST/PUT/DELETE /api/v1/boms ; POST /boms/:id/enable ; GET /boms/:id/tree
CRUD /api/v1/routings, /work-centers, /customers, /suppliers
POST /boms-validate   # 环引用校验（避免与 /boms/:id 路由树冲突，独立路径）
```

## 4. ERP 交易

```
# 销售订单
CRUD /api/v1/sales-orders  ; POST /:id/audit ; POST /:id/void ; POST /:id/close
POST /:id/create-shipment  → 生成 WMS 出库草稿（返回 stock_order id）
# 请购 / 采购
CRUD /api/v1/purchase-requests ; POST /:id/audit ; POST /:id/convert-to-po
CRUD /api/v1/purchase-orders ; POST /:id/audit ; POST /:id/arrive → 生成到货入库草稿
# MRP
POST /api/v1/mrp/run          {factory_id, material_scope?} → {suggestions[], run_id}
GET  /api/v1/mrp/runs/:id     # 建议明细（含需求来源穿透）
POST /api/v1/mrp/suggestions/convert  {ids:[], to: "wo"|"pr"} → 生成的单据号
```

## 5. MES

```
# 工单
CRUD /api/v1/work-orders
POST /:id/release   # 下达（固化快照+生成工序）
POST /:id/pause | /resume | /finish | /void | /close
POST /:id/picking-request   # 生成领料出库草稿
POST /:id/finish-receipt    # 完工报交生成入库草稿
# 派工/工序
GET  /api/v1/wo-operations?work_order_id= | ?mine=1
POST /api/v1/wo-operations/:id/assign {user_id,group_id}
POST /api/v1/wo-operations/:id/start | /finish
# 报工（PDA 主接口）
POST /api/v1/production-reports {work_order_no, seq, qty_ok, qty_defect, defect_codes[], hours, client_token}
GET  /api/v1/production-reports?work_order_id=
# 质量
CRUD /api/v1/qc-schemes, /defect-codes
POST /api/v1/qc-records ; POST /:id/audit {result, disposal}
GET  /api/v1/trace/batch/:batchNo   # 批次追溯链
GET  /api/v1/trace/work-order/:id
# 看板
GET  /api/v1/kanban/board          # 在制墙
GET  /api/v1/kanban/today          # 今日产量/合格率
```

## 6. WMS

```
# 库存
GET  /api/v1/stocks?factory_id&warehouse_id&material_id&batch_no&dims=…  # 台账
GET  /api/v1/stocks/available?material_ids=1,2   # 可用量（MRP/销售复用）
GET  /api/v1/stock-movements?material_id&source_doc_id=   # 流水穿透
# 出入库单（doc_type 路径区分语义，底层同资源）
CRUD /api/v1/stock-orders/:docType(rcv|shp|mv)  ; POST /:id/post  # 过账
POST /:id/post-lines {lines:[{line_id,qty,location_id,batch_no}]}  # 分次执行
# 盘点
CRUD /api/v1/stocktakings ; POST /:id/start | /finish-counting → 生成差异调整草稿
POST /api/v1/stocktakings/:id/lines/:lineId {counted_qty}
# 预占（一般由单据审核自动触发，API 供查询/释放）
GET  /api/v1/reservations ; POST /:id/release
```

## 7. 系统

```
GET /healthz | /readyz | /metrics
GET /api/v1/dashboard/summary   # 工作台卡片：待办/在制/今日产量/库存预警
```

## 8. 错误码段

| 段 | 含义 |
|---|---|
| 0 | 成功 |
| 1xxx | 认证/权限 |
| 2xxx | 参数/校验 |
| 3xxx | 业务规则（3001 库存不足、3002 状态迁移非法、3003 版本冲突、3004 BOM 环引用、3005 幂等冲突…） |
| 4xxx | 数据库/系统 |
