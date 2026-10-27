# 技术架构设计（Technical Architecture）

> 对应 NFR 分册；架构蓝本：gin-vue-admin 分层实践 + carbon 单库领域建模理念，按本产品多租户需求重构。

## 1. 总体架构

```
                         ┌──────────────────────────────┐
        浏览器 PC ──────▶ │  Nginx / Traefik（TLS 终止）   │
        PDA PWA  ──────▶ │  静态资源：React 构建产物        │
                         └──────────────┬───────────────┘
                                        │ /api/v1/*
                         ┌──────────────▼───────────────┐
                         │   EvolverMon API（Go/Gin）×N   │  ← 无状态，水平扩展
                         │  ┌──────────────────────────┐ │
                         │  │ middleware: recover/cors │ │
                         │  │ / logging(trace) / auth  │ │
                         │  │ / tenant / rbac / audit  │ │
                         │  └───────────┬──────────────┘ │
                         │  handler → service → repo     │
                         │  （业务事务在 service 层）        │
                         └───────┬───────────────┬──────┘
                                 │               │
                     ┌───────────▼─────┐   ┌─────▼──────────┐
                     │ PostgreSQL 16   │   │ Redis (可选 M2)  │
                     │ 业务库+审计+流水  │   │ 缓存/锁/队列      │
                     └─────────────────┘   └────────────────┘
```

## 2. 后端分层（Go + Gin）

> **M1 实施修订**：为控制初版交付节奏，`handler` 与 `service` 合并为 `internal/api`（HTTP 编解码与业务规则同文件按域组织）；`repo` 职责由 GORM + `data.ScopeFor` 租户过滤作用域承担（`internal/data`）。模块长大后再拆分三层的迁移路径已在每域文件内预留（业务函数均为方法，可平移）。

```
backend/
├── cmd/server/main.go          # 装配：config→db(等PG→AutoMigrate→seed)→router→run
├── internal/
│   ├── config/                 # 环境变量（EM_*），敏感配置不入库（NFR-033）
│   ├── pkg/                    # 通用件：jwtx(JWT)、response(统一包/错误码)、测试
│   ├── middleware/             # trace/cors/auth(JWT→加载权限)/rbac(Require)
│   ├── model/                  # GORM 实体（按域文件）+ BaseModel（审计列/乐观锁）
│   ├── data/                   # DB 初始化、租户过滤作用域、单号服务、审计助手
│   ├── crud/                   # 泛型 CRUD 引擎（列表/详情/增删改+租户过滤+乐观锁）
│   ├── api/                    # 业务接口（auth/platform/master/erp/mrp/mes/wms/board）
│   └── router/                 # 路由注册 + 权限点绑定（装配冒烟测试保路由无冲突）
├── seed/                       # 幂等种子：菜单/角色/超管/双工厂/演示主数据
├── Dockerfile                  # 多阶段构建 → ~20MB 单二进制
└── cmd/server/main.go
```

**关键决策**：
1. **事务边界在 api 层业务函数**：一个业务动作（如过账）一个事务。
2. **租户上下文中间件**：JWT 解出 user → 加载租户/当前工厂/权限点 → 注入 context；查询统一走 `data.ScopeFor(ac, factoryScoped)` 注入 `tenant_id/factory_id` 条件，**杜绝**手写查询漏条件。
3. **单号服务**：`doc_number_rules` 表原子 `UPDATE current_seq+1`，无竞态。
4. **幂等**：报工以 `client_token` 唯一索引去重（FR-MES-032）；通用写接口幂等头为 M2 项。
5. **状态机**：代码内显式校验 from→to，非法迁移返回 409（乐观锁 version）。

## 3. 前端架构（React 18 + TS + Vite）

```
frontend/
├── src/
│   ├── api/            # axios 实例（token 注入/401 刷新/错误吐司）+ 按域模块
│   ├── stores/         # zustand：auth(令牌/用户/当前工厂)、ui(菜单折叠)
│   ├── components/     # CrudTable（通用列表：列配置+搜索+分页）、StatusTag、PageHeader…
│   ├── layouts/        # BasicLayout（侧边菜单+顶栏工厂切换）/ PdaLayout
│   ├── pages/          # login / dashboard / system / master / erp / mes / wms
│   ├── router/         # 懒加载路由，按菜单权限过滤
│   └── utils/
```

- 组件库 **Ant Design 5**；状态 **zustand**（轻量）；请求 **axios**；图表 **recharts**。
- **通用 CRUD 引擎**：列表页=列定义+API 端点配置 → `CrudTable`，模块页开发成本压到小时级（支撑 NFR-070）。
- PDA 模式：同一 SPA 内 `/pda/*` 路由 + 大按钮触屏布局（PdaLayout），不另建工程。

## 4. 多租户与多工厂实现

| 关注点 | 方案 |
|---|---|
| 租户识别 | JWT claim `tenant_id`；登录后可切换 `factory_id`（claim 中带可访问工厂列表） |
| 数据隔离（自部署） | 每租户独立库（连不同 DSN） |
| 数据隔离（托管） | 共享库 + repo 层强制过滤 + 预留 RLS |
| 上下文传递 | `context.Context` 携带 `TenantCtx{TenantID,FactoryID,UserID,DataScope}` |
| 越权测试门禁 | 集成测试矩阵：每个业务 API ×（无token/错租户/错工厂/无权限点）→ 必须 401/403 |

## 5. 高并发设计要点

1. **报工路径**：单表 INSERT + 工序累计列原子 UPDATE（`UPDATE … SET qty_ok=qty_ok+$1`），无长事务；累计读走同一行（少量争用可接受）。
2. **库存过账**：`SELECT … FOR UPDATE` 锁台账行（工厂+仓+位+料+批唯一键）→ 写流水 → 更新台账 → 同事务提交；天然防超卖。
3. **连接池**：pgx 默认池，max conns = 50×N 实例，按 DB 上限反推实例数。
4. **读多写少列表**：分页必须；预留 Redis 物料/BOM 缓存（M2）。
5. **审计异步化候选**：M1 同步写（简单正确），量级上来后换 channel buffer+批量落库（M2）。

## 6. 领域事件与扩展（二开友好）

- 单据状态迁移后发布**领域事件**（进程内轻量 bus + 可选 webhook 出口）：`wo.released`、`stock.posted`、`order.audited` …
- 二开不改核心：① 自定义字段（表 extra jsonb）；② webhook 订阅；③ 报表用只读 SQL 视图。

## 7. CI/CD 与质量门禁

- GitHub Actions：lint(golangci-lint, eslint) → test(coverage 门禁) → build(双平台二进制+镜像) → govulncheck。
- 集成测试：docker-compose 起 PG → 跑 API 全链路（含租户越权矩阵）。
- 发布：语义化版本 tag → 自动出 Release（二进制+镜像+CHANGELOG）。
