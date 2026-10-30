# EvolverMon 微服务架构演进方案（v1）

> 状态：方案已评审待执行 · 关联：[architecture.md](architecture.md) · [infra-ports.md](infra-ports.md) · 执行计划见 `docs/04-delivery/microservices-execution-plan.md`（内部）
>
> 原则一句话：**先拆边界、再拆部署、最后拆数据；小工厂单机一键部署体验不允许劣化。**

## 1. 现状分析（v0.7.0）

### 1.1 当前后台形态

| 组件 | 现状 | 位置 |
|---|---|---|
| 后端 | **单体二进制**，一个进程承担：HTTP API（60+ 路由 / 16 个 api 模块）、webhook 投递 worker、定时 MRP ticker、版本化迁移、演示种子 | `backend/cmd/server/main.go` |
| 数据库 | **单库单 schema**（`evolvermon`），GORM + `schema_migrations` 增量迁移 | `backend/internal/data/` |
| 前端 | React18 SPA（含 PDA / 大屏 / 打印） | `frontend/` |
| 中间件 | 默认零依赖（进程内 memory）；Broker/Cache/Locker 三端口可切 RabbitMQ/Kafka/Redis | `backend/internal/infra/` |
| 事件 | 领域事件双写：DB 投递箱（webhook 出口）+ `evolvermon.events` 总线主题（Key=tenant-{id} 保序） | `backend/internal/events/` |
| 部署 | docker-compose 单机一键（`--profile full` 可选中间件） | `docker-compose.yml` |

### 1.2 已具备的微服务"预埋件"

- **端口-适配器基础设施**：Broker/Cache/Locker 三端口，业务代码不感知中间件——跨进程通信与分布式锁已就绪。
- **事件双写 + 投递箱**：`events.Publish` 的 DB outbox 雏形已存在，泛化后即跨服务最终一致性的基础。
- **分布式锁**：定时 MRP 已用 `mrp-auto` 锁做多副本互斥——worker 独立部署的模式已验证。
- **按域组织的代码**：api/model 文件已按 platform/master/erp/mes/wms/integration 分域，重组成本低。

### 1.3 差距（为什么还不是微服务）

1. 单进程：任何域的变更都要整体重新部署；后台 worker 与 API 抢占同一进程资源。
2. 单库单 schema：域间通过外键 + JOIN + 同一本地事务强耦合，无法按域独立伸缩/备份/迁移。
3. 无网关与路由分层：前端直连单后端。
4. 无统一运维面：没有服务注册、健康聚合、启停控制、监控告警——多服务后人工运维不可行。
5. 单据关键流程（报工→库存过账→成本归集、销售审核→预占、采购入库→过账）跨域共事务——**这是拆分的核心难点**。

## 2. 目标架构

### 2.1 总体形态：一套代码，两种部署

| 形态 | 进程拓扑 | 面向 |
|---|---|---|
| **Lite（默认，保留）** | 单二进制 `evolvermon`（各域模块编译聚合，进程内 memory broker/cache，单库多 schema） | 小工厂自托管、开源用户零运维（NFR-020 形态① 不劣化） |
| **Micro** | 网关 + 8 个独立服务 + 分 schema/分库 PG + 可插拔中间件 | 多副本水平扩展、按域伸缩、大客户/多租户 SaaS 化 |

同一份域代码（`internal/<domain>/...`），Lite 用 `cmd/server` 聚合注册，Micro 用 `cmd/<svc>` 各自装配。**不允许出现两套业务代码。**

### 2.2 服务拆分（目标 9 个部署单元）

| # | 服务 | 职责（现 api/model 归属） | 数据归属 | 拆分梯队 |
|---|---|---|---|---|
| 1 | **gateway** | 对外入口：路由、JWT 校验、租户头注入、限流（Phase 1 由现有 nginx 兼任，不新增组件） | 无 | T1 |
| 2 | **identity** | 认证、租户、用户、RBAC、审计日志接收、单号规则（api/auth、platform；model/platform） | `platform` schema | T1 |
| 3 | **integration** | Webhook 出口、设备遥测、智能建议（api/m6；events worker；model/m6） | `integration` schema | T1 |
| 4 | **scheduler** | 定时 MRP、超期采购提醒、对账任务（后台任务进程，无 HTTP） | 无自有库（锁 + 调用各服务 API） | T1 |
| 5 | **ops-admin**（新增） | 统一管理平台：服务注册/健康/启停/监控/告警/运维审计 | `ops` schema | T1 |
| 6 | **master-data** | 物料、BOM、工艺、工作中心、客商（api/master；model/master） | `master` schema | T2 |
| 7 | **core-erp** | 销售、采购、请购、MRP、ATP、排产、财务凭证（api/erp、mrp、plan、finance） | `erp` schema | T3 |
| 8 | **core-mes** | 工单、报工、质检、成本、看板、追溯、打印（api/mes、quality、trace、board、print） | `mes` schema | T3 |
| 9 | **core-wms** | 库存台账/流水、批次、序列号、盘点、厂间调拨、IQC 三桶（api/wms、transfer） | `wms schema` | T3 |

拆分梯度理由（耦合度实测自代码）：
- **T1 低耦合**：identity 只被依赖验签（JWT 离线校验，无运行时依赖）；integration 纯事件消费方；scheduler 已是后台任务。
- **T2 中耦合**：master-data 被 ERP/MES/WMS 高频读（物料/BOM/工艺）——需配套读缓存 + 事件失效。
- **T3 高耦合**：报工（MES）事务内做 WMS 库存过账 + 预占核销；销售审核（ERP）事务内做 WMS 预占；采购入库（ERP）事务内做 WMS 过账。当前 200 并发报工 P99=32ms 依赖行锁本地事务，**直接拆库必然引入分布式事务与性能回归**。

### 2.3 核心域（T3）拆分策略：一步到位拆库，跨域一律走 API

**决策（2026-10-31 定稿）：core-erp / core-mes / core-wms 拆分时直接"一进程一库"，不设"同实例跨 schema 共事务"过渡态。** 跨域一致性一律通过 **内部 API（同步、幂等）+ 本地事务 + 补偿（Saga）** 实现：

```
报工(mes)
  │ 1. 本地事务：保存报工单(pending)
  │ 2. 同步调用 POST /internal/wms/stock-postings（幂等键=report_id）
  ▼
wms 过账成功 ──▶ mes 本地事务提交(PostedAt 落地)
  │                  │
  │ mes 提交失败 ─▶ 调 wms 冲销 API（幂等键同上）+ 告警
  │ wms 不可用   ─▶ 报工单停留在 pending，明确报错可重试（绝不静默丢）
  └─ 异步：outbox 事件 work.order.posted → 成本归集/看板/integration
```

三个关键链路的实现约定：

| 链路 | 同步 API（强一致段） | 补偿 / 事件（最终一致段） |
|---|---|---|
| 报工（MES→WMS） | `POST /internal/wms/stock-postings`，幂等键=报工ID | MES 提交失败→冲销 API；WMS 不可用→报工 pending 可重试；`work.order.posted` 事件驱动成本归集 |
| 销售审核（ERP→WMS） | `POST /internal/wms/allocations`，幂等键=订单ID | 订单作废/关闭→`DELETE /internal/wms/allocations` 释放预占 |
| 入库过账（ERP/MES→WMS） | 过账 API（采购入库/完工入库/退料） | 冲销 API（幂等）+ 每日对账兜底 |

**内部 API 规范**（详见 2.6）：`/internal/*` 前缀仅限服务间调用（网关不对外路由）；内部服务令牌认证；**幂等键必填**（消费侧按键去重落表）；超时预算与重试上限显式配置；`traceparent` 全链路透传；错误码严格区分**业务拒绝**（不重试、走人工/补偿）与**系统故障**（可安全重试）。

### 2.4 库拆分策略

| 阶段 | 数据库拓扑 | 说明 |
|---|---|---|
| A（Phase 0-1） | 单库多 schema：`platform` `master` `erp` `mes` `wms` `integration` `ops` | 仅为迁移铺路的中间状态：迁移目录按服务拆分（`migrations/<service>/NNN.sql`），每服务只管自己的迁移集；跨 schema FK/JOIN 禁止（测试静态检查） |
| B（Phase 2） | T1/T2 服务物理分库（identity/master/integration/ops 各自 database） | 低耦合域先拆库；跨库读走 API+缓存 |
| C（Phase 3，**定稿必做**） | core 三库分离（core_erp / core_mes / core_wms 各自 database） | 拆分即拆库（见 2.3），跨域事务走同步 API + 补偿，不做同库过渡态 |

**跨域数据访问铁律**：
1. 禁止跨 schema 外键与 JOIN（新增 `TestNoCrossSchemaDependency` 静态扫描 GORM 模型表归属与 SQL）。
2. 跨域读：内部服务客户端（HTTP/JSON）+ Cache 端口缓存 + 事件失效。
3. 跨域写：领域事件 + **泛化 outbox**（现有 `WebhookDelivery` 投递箱模式泛化为通用 `outbox` 表 + relay，消费方按消息 ID 幂等）。
4. 兜底：对账任务（延续"账实对账 0 差异"文化，新增跨服务一致性对账：预占 vs 订单、事件发送 vs 消费）。

### 2.5 统一管理平台（ops-admin）设计

#### 2.5.1 定位
面向平台管理员（`platform-admin` 角色）的统一运维面：**看到所有服务、控制所有服务、但不承载业务流量**。Lite 模式下它管理的"服务列表"退化为单进程内的模块健康 + 后台 worker 开关，同一套 UI。

#### 2.5.2 功能

| 模块 | 内容 |
|---|---|
| 服务注册表 | 静态 `registry.yml`（服务名/端口/健康端点/启停驱动/依赖）+ 运行时状态（健康/版本/实例数/启动时间） |
| 健康聚合 | 轮询各服务 `/healthz`（存活）`/readyz`（就绪：DB/中间件连通），10s 周期；状态灯 + 事件记录（何时变 down/up） |
| **启停控制** | start/stop/restart/scale，按 **Driver 接口**适配运行环境；二次确认 + 全量运维审计（谁、何时、对哪个服务、什么操作、结果） |
| 监控 | 抓取各服务 `/metrics`（Prometheus 格式）内置短期存储（24h）并出图：QPS、P99、错误率、Goroutine/内存、队列积压、outbox 延迟；可选外接 Prometheus+Grafana（配置即切） |
| 日志 | Docker driver：`docker logs` tail；process driver：日志文件 tail；Phase 4 可选接 Loki |
| 告警 | 规则：服务 down、5xx 率>1%、P99>阈值、webhook/outbox 积压、磁盘；通道：管理平台通知 + webhook（复用 integration 的 HMAC 签名投递） |
| 运维审计 | 独立 `ops` schema，append-only |

#### 2.5.3 启停 Driver 接口（端口-适配器，与 infra 同风格）

```go
// internal/ops/driver
type Driver interface {
    Start(ctx, service string, opts StartOpts) error   // docker compose up -d <svc> / 进程拉起 / k8s scale
    Stop(ctx, service string) error
    Restart(ctx, service string) error
    Scale(ctx, service string, n int) error            // k8s/compose 副本数
    Status(ctx, service string) (InstanceState, error)
}
```

| Driver | 运行环境 | 实现 |
|---|---|---|
| `compose` | docker-compose Micro 形态 | Docker Engine API（挂载 /var/run/docker.sock，仅 ops-admin 容器） |
| `process` | Lite/裸机多进程 | `os/exec` 拉起 `cmd/<svc>` + PID 文件 |
| `k8s` | Phase 4 | Kubernetes API（scale/rollout） |

fail-fast 原则延续：Driver 配置不可用 → ops-admin 启动即报错，不静默降级。

#### 2.5.4 安全（最高权限面）
- 独立 JWT audience（`ops`），仅 `platform-admin` 角色；
- 默认仅内网监听（不经 gateway 对公网暴露）；如需远程访问走 VPN/反代 + IP 白名单；
- 启停操作：二次确认 + 审计；可选开启「重启需双人在座确认」；
- ops-admin 自身无状态化设计：registry 来自文件、状态来自探测——**它宕机不影响任何业务流量**（只丢观测，不断服务）。

#### 2.5.5 前端
现有 React 加「系统运维」菜单（仅平台管理员可见，符合暖色系主题）：
- 服务总览页：卡片式（状态灯 / 版本 / 实例数 / CPU·内存 / 最近错误 / 启停按钮）
- 服务详情页：监控图表、日志 tail、健康事件时间线
- 告警中心：规则管理 + 历史告警
- 运维审计页

### 2.6 服务间通信

| 通道 | 选型 | 理由 |
|---|---|---|
| 同步调用 | **HTTP/JSON** + 服务名寻址（compose DNS / K8s Service / Lite 进程内直调） | 延续 Gin+JSON 栈与 api-design.md 风格，零新工具链；内部客户端统一 `internal/client` 接口，留 gRPC 演进位 |
| 异步事件 | 复用 **infra.Broker**（RabbitMQ/Kafka/memory 适配器已有），每服务独立消费组 | v0.7.0 投资直接复用；Lite 模式退化为进程内 bus，行为一致 |
| 最终一致 | outbox + 消费幂等表 + 对账任务 | 见 2.4 |
| 链路追踪 | W3C `traceparent` 头透传（中间件统一注入/提取），可选接 OTel Collector | 轻量起步，不绑全家桶 |

### 2.7 技术选型红线（延续项目章程）

- 不引入服务注册中心（Consul/Nacos/Eureka）——compose DNS + registry.yml 静态注册足够，自托管用户零运维；
- 不引入 Service Mesh——网关即 nginx（已有 web 容器兼任），K8s 阶段用 Ingress；
- 不引入新语言/新框架——全 Go+Gin+GORM，服务化只是部署拓扑变化；
- 服务数量克制：**上限 9 个部署单元**，不再细分（开源项目维护成本约束）；
- fail-fast：任何服务配了远端依赖不可达 → 启动即失败（延续 infra 原则）。

## 3. 关键风险与对策

| # | 风险 | 对策 |
|---|---|---|
| 1 | 拆库后跨域事务失败/性能回归（现 200 并发 P99=32ms 依赖本地行锁事务） | 同步内部 API 幂等设计 + 补偿冲销 + 每日对账兜底；报工链路保留"pending→确认"两段式，WMS 不可用时明确可重试而非静默失败；每阶段压测门槛（P99 ≤ 基线×1.5）不达标先优化（连接池/批量过账/内网 keep-alive）再进下一阶段 |
| 2 | 小工厂部署体验劣化（违背 NFR-020 与免费+顾问商业模式） | Lite 单二进制形态永久保留且为默认；`verify-golden-path.sh` 必须双形态通过 |
| 3 | 事件乱序/重复消费导致数据不一致 | 总线 Key=tenant-{id} 保序（已有）；消费幂等表；跨服务对账任务纳入验收 |
| 4 | ops-admin 成为安全突破口（能启停一切） | 2.5.4 独立鉴权域 + 内网监听 + 审计 + 二次确认 |
| 5 | 开源维护成本上升（人少服务多） | 单仓 monorepo 统一 CI；一套代码两形态；服务数上限 9；Lite 用户无感知 |
| 6 | 迁移脚手架分裂 | 迁移目录按服务拆但工具统一（`EM_MIGRATE_ONLY` 扩展为按服务执行） |

## 4. 与现有设计的关系

- [architecture.md](architecture.md)：单体分层描述保持为 Lite 形态文档，本方案为 Micro 形态演进总纲；
- [infra-ports.md](infra-ports.md)：Broker/Cache/Locker 直接复用为跨服务通信底座，不推翻；
- [database-design.md](database-design.md)：表结构不变，仅增 schema 归属标注与迁移目录重组；
- PRD（docs/02-prd）：功能需求零变化——本方案是部署架构演进，不改业务行为；所有 FR 验收断言（64 项）双形态必须全绿。
