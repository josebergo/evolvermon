# 核心业务流程（Core Flows）

> 状态机与流程图统一用 Mermaid 维护，代码中的状态迁移必须与本文档一致（变更走 PR 同步修改）。

## 1. 端到端主流程（Order to Delivery）

```mermaid
flowchart TD
  A[销售订单草稿] -->|审核| B[已审核→预占库存 S]
  B --> C{MRP 运算}
  C -->|缺成品产能| D[生产建议→工单]
  C -->|缺原料| E[采购建议→请购→采购订单]
  D --> F[工单下达→工序生成]
  F --> G[领料出库单→WMS 过账扣料]
  G --> H[派工→扫码报工]
  H --> I{末工序合格数≥计划?}
  I -->|是| J[工单已完工]
  J --> K[报交入库单→WMS 过账增库存]
  E --> L[到货入库单→WMS 过账增库存]
  K --> M[销售发货单→WMS 过账扣库存]
  L --> M
  M --> N[订单全部发货→已完成→关闭]
```

## 2. 关键状态机

### 2.1 销售订单
```mermaid
stateDiagram-v2
  [*] --> 草稿
  草稿 --> 已审核: 审核(校验客户/物料有效)
  草稿 --> 作废: 删除或作废
  已审核 --> 部分发货: 行发货 qty>0
  部分发货 --> 已完成: 全行 sh发完
  已完成 --> 已关闭: 关闭
  已审核 --> 已关闭: 手工关闭(留未发量)
```

### 2.2 工单
```mermaid
stateDiagram-v2
  [*] --> 草稿
  草稿 --> 已下达: release 固化BOM/工艺快照+生成工序
  已下达 --> 执行中: 首次报工/开工
  执行中 --> 暂停: pause(留原因)
  暂停 --> 执行中: resume
  执行中 --> 已完工: 末工序合格≥计划 或 强制完工
  已完工 --> 已入库: 报交入库单过账
  已入库 --> 已关闭: close
  草稿 --> 作废: void(未报工才可)
  已下达 --> 作废: void(需先撤工序)
```

### 2.3 出入库单
```mermaid
stateDiagram-v2
  [*] --> 草稿
  草稿 --> 执行中: 首行过账
  执行中 --> 已完成: 全行 done_qty≥qty
  草稿 --> 作废
```

## 3. 库存过账事务（伪代码，防超卖）

```
BEGIN
  -- 幂等检查（client_token / 单据行+数量组合）
  锁台账行: SELECT * FROM stocks
            WHERE factory_id=? AND warehouse_id=? AND material_id=? AND batch_no=?
            FOR UPDATE;  -- 无行则 INSERT ... ON CONFLICT DO NOTHING 后重取
  校验: 出库时 qty_on_hand - qty_reserved(本次核销量) >= qty  (负库存参数)
  写流水: INSERT stock_movements(before_qty, after_qty, biz_type, source_doc)
  更新台账: UPDATE stocks SET qty_on_hand = after_qty
  回写单据行 done_qty / 单据状态机推进
  核销预占(如有): UPDATE stock_reservations ...
  写审计
COMMIT
```

## 4. MRP 计算算法（M1 简化全量）

```
输入: 已审核销售订单(净需求起点), 可用库存, 在途(已审未入库采购), 在制(未完工工单产出)
1. 计算低层码 LLC(material)
2. 按销售订单行生成毛需求 P0: (material, qty, due_date)
3. 按层从低到高处理:
   net = 毛需求 - (可用库存 + 在途 + 在制)   // 数量守恒逐层滚动
   if net > 0:
     生成计划建议 (生产建议 or 采购建议, qty=net, date=due-提前期)
     按 BOM 展开子项毛需求: child_qty = net * (1+损耗率) * 用量
4. 输出建议列表(含需求来源链, 供穿透)
```

## 5. 批次追溯链

```
成品批次 ──报交──▶ 工单 ──领料单──▶ 原料批次 ──采购入库──▶ 供应商
   ▲                                                │
   └────────── 工序报工(人/时间/不良) ◀──────────────┘
查询入口: /trace/batch/:batchNo 与 /trace/work-order/:id
```
