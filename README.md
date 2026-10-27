# EvolverMon · 开源工业一体化管理平台（ERP + MES + WMS）

**简体中文** | [English](README.en.md)

**作者：码禾数字（Mahe Digital）**

> **一句话**：用 Go 的性能 + React 的体验，让中小制造企业 5 分钟跑通「销售订单 → MRP → 工单 → 报工 → 质检 → 入库 → 发货」全流程闭环。多工厂多租户 · PostgreSQL · Apache-2.0 免费商用 · 暖色高对比 UI。

技术栈：Go 1.27 + Gin + GORM · React 18 + TypeScript + Vite + Ant Design 5 · PostgreSQL 16

## 快速开始

### 方式一：Docker Compose（推荐，一条命令）

```bash
docker compose up -d --build
# 打开 http://localhost —— 首次启动自动建表+写入演示数据（双工厂/物料/BOM/工艺/初始库存）
```

### 方式二：本地开发（前后端两个独立文件夹）

```bash
# 1. 准备 PostgreSQL 16，建库 evolvermon
# 2. 启动后端 backend/（:8080，可托管 frontend/dist 实现单二进制部署）
cd backend
export EM_DATABASE_DSN="host=127.0.0.1 port=5432 user=postgres password=postgres dbname=evolvermon sslmode=disable"
export EM_JWT_SECRET="dev-secret"
go run ./cmd/server

# 3. 启动前端 frontend/（:5173，已配置 /api 代理到 8080）
cd frontend
npm install && npm run dev
```

### 端到端自动验证

```bash
bash scripts/verify-golden-path.sh [base_url]   # 49 项断言：M1–M6 全链路 + 越权矩阵
bash scripts/load-report.sh [base_url] 200      # 200 并发报工压测（基线 P99=32ms）
```

## 演示账号

| 账号 | 密码 | 角色 | 可体验 |
|---|---|---|---|
| `admin` | `Admin@123` | 超级管理员 | 全部功能、工厂切换、集团视图、Webhook |
| `planner` | `Demo@123` | 计划员 | MRP / ATP / 排产 / 工单 / 采购 |
| `supervisor` | `Demo@123` | 车间主管 | 工单/派工/报工/看板/大屏 |
| `keeper` | `Demo@123` | 仓管员 | 出入库/PDA/盘点/厂间调拨 |
| `qc` | `Demo@123` | 质检员 | IQC/FQC/处置/序列号追溯 |
| `sales` | `Demo@123` | 销售员 | 销售订单/ATP 交期承诺 |

## 功能地图（M1–M6 已全部实现并验证）

- **平台底座**：多租户/多工厂（顶栏切换）、RBAC+数据范围、JWT 双令牌、审计日志、系统参数、并发安全单号、操作水印、中英切换
- **主数据**：物料（批次/序列号/安全库存/提前期）、BOM（多级/版本/单一启用/树形/防环）、工艺路线、工作中心、客商
- **ERP**：销售订单（审核预占/发货）、请购→采购（合并转单/到货回写）、**MRP**（净需求多层展开/穿透来源/一键转单/定时运行）、**ATP 交期承诺**
- **MES**：工单全生命周期（快照/派工/幂等报工/完工/入库）、工序排产时间、**有限产能排产+甘特+派工看板**、车间看板/大屏（暖色暗底轮播）、IQC/FQC 自动质检、返工/报废处置、**工单实际成本归集**、批次+序列号双追溯、设备遥测接入
- **WMS**：库存台账+流水（只增不改）、出入库分次过账（行锁防超卖/负库存拦截/**FIFO 自动拣批**）、预占（占用/核销/释放）、移库、盘点（快照/盲盘/差异自动调整）、**厂间调拨**、中文单据打印（送货单/领料单/工单路由卡）
- **集成与智能**：Webhook 事件出口（HMAC 签名/重试/投递日志）、财务凭证导出（应收应付/CSV）、智能建议（缺料/交期/产能）
- **PDA**：大按钮触屏的报工页 + 仓储作业页（收货/领料）

版本细节见 [CHANGELOG.md](CHANGELOG.md)。质量基线：金路径 **49/49**、账实对账 0 差异、200 并发报工 **P99=32ms**。

## 项目结构

```
evolver-mon/
├── backend/                 # Go+Gin API（cmd/server + internal/{api,crud,data,events,model,…} + seed）
├── frontend/                # React+TS+Vite 前端（src/pages 按域组织 + 通用 CrudTable 引擎）
├── docs/                    # 功能设计文档（见下表）
├── scripts/                 # 金路径验证 / 并发压测
├── docker-compose.yml       # 一键部署
└── LICENSE                  # Apache-2.0
```

## 功能设计文档

| 文档 | 内容 |
|---|---|
| [PRD 总册](docs/02-prd/00-prd-master.md) | 角色矩阵、跨领域规范（多租户/单据状态机/审计/权限编号体系） |
| PRD 分册 | [平台](docs/02-prd/01-prd-platform.md) · [ERP](docs/02-prd/02-prd-erp.md) · [MES](docs/02-prd/03-prd-mes.md) · [WMS](docs/02-prd/04-prd-wms.md) · [非功能](docs/02-prd/05-prd-nfr.md) |
| [架构设计](docs/03-design/architecture.md) | 分层/多租户实现/高并发设计/事件出口 |
| [数据库设计](docs/03-design/database-design.md) | 全表 DDL 规范、索引与约束清单 |
| [API 设计](docs/03-design/api-design.md) | REST 约定、错误码、全端点 |
| [核心流程](docs/03-design/core-flows.md) | 端到端流程/状态机/MRP 算法/过账事务 |
| [CHANGELOG](CHANGELOG.md) | M1–M6 各版本功能清单与质量基线 |

## 参与贡献

- 二开友好：通用 CRUD 引擎让一个资源页 ≈ 40 行代码；前端 `CrudTable` 同构；自定义字段（jsonb extra）与 Webhook 提供不改核心的扩展点。
- 质量门禁：`go build ./... && go vet ./... && go test ./...`；前端 `npm run build`（tsc 严格模式）；金路径脚本回归。
- 行为准则：以真实工厂场景驱动（PRD 用户故事），不接受"抄竞品功能"式 PR。

---

*EvolverMon — 让每家工厂都用得起的世界级开源制造管理软件。*  
*作者 / Author：码禾数字*
