# EvolverMon · Open-Source Industrial Management Platform (ERP + MES + WMS)

[简体中文](README.md) | **English**

**Author: 码禾数字**

> **In one sentence**: the performance of Go plus the experience of React, letting small and mid-sized manufacturers run the full loop — *Sales Order → MRP → Work Order → Shop-Floor Reporting → QC → Stock-In → Shipping* — in 5 minutes. Multi-tenant & multi-factory · PostgreSQL · Apache-2.0 free for commercial use · warm high-contrast UI.

Tech stack: Go 1.27 + Gin + GORM · React 18 + TypeScript + Vite + Ant Design 5 · PostgreSQL 16

## Quick Start

### Option 1: Docker Compose (recommended, one command)

```bash
docker compose up -d --build
# Open http://localhost — first start auto-creates tables and demo data
# (two factories, materials, BOMs, routings, opening stock)
```

### Option 2: Local development (backend and frontend in two separate folders)

```bash
# 1. Prepare PostgreSQL 16 and create database "evolvermon"
# 2. Start the backend in backend/ (:8080; can also serve frontend/dist for single-binary deployment)
cd backend
export EM_DATABASE_DSN="host=127.0.0.1 port=5432 user=postgres password=postgres dbname=evolvermon sslmode=disable"
export EM_JWT_SECRET="dev-secret"
go run ./cmd/server

# 3. Start the frontend in frontend/ (:5173, /api proxied to 8080)
cd frontend
npm install && npm run dev
```

### End-to-end automated verification

```bash
bash scripts/verify-golden-path.sh [base_url]   # 49 assertions: full M1–M6 chain + privilege matrix
bash scripts/load-report.sh [base_url] 200      # 200-concurrent reporting load test (baseline P99 = 32ms)
```

## Demo Accounts

| Account | Password | Role | Try it |
|---|---|---|---|
| `admin` | `Admin@123` | Super admin | Everything, factory switching, group view, webhooks |
| `planner` | `Demo@123` | Planner | MRP / ATP / scheduling / work orders / purchasing |
| `supervisor` | `Demo@123` | Shop supervisor | Work orders, dispatch, reporting, kanban, big screen |
| `keeper` | `Demo@123` | Warehouse keeper | In/out orders, PDA, stocktaking, inter-factory transfers |
| `qc` | `Demo@123` | QC inspector | IQC/FQC, disposal, serial-number traceability |
| `sales` | `Demo@123` | Sales | Sales orders, ATP promise date |

## Feature Map (M1–M6 all implemented and verified)

- **Platform**: multi-tenant / multi-factory (switch in top bar), RBAC + data scope, dual JWT tokens, audit log, system parameters, concurrency-safe document numbering, operation watermark, Chinese/English switch
- **Master data**: materials (batch / serial number / safety stock / lead time), BOM (multi-level, versioned, single-active, tree view, cycle check), routings, work centers, customers & suppliers
- **ERP**: sales orders (reserve on audit, shipping), purchase requests → purchase orders (merge & convert, receipt write-back), **MRP** (multi-level net-requirement explosion, source drill-down, one-click conversion, scheduled runs), **ATP** promise date
- **MES**: full work-order lifecycle (BOM snapshot, dispatch, idempotent reporting, finish, stock-in), operation schedule times, **finite-capacity scheduling + gantt + dispatch board**, shop-floor kanban & big screen (warm dark theme, rotating panels), automatic IQC/FQC, rework/scrap disposal, **actual WO costing** (material + labor + overhead), batch & serial-number traceability, device telemetry ingestion
- **WMS**: stock ledger + immutable movements, partial posting with row-lock anti-oversell, negative-stock blocking, **FIFO auto batch picking**, reservations (hold/consume/release), transfers, stocktaking (snapshot / blind count / auto difference adjustment), **inter-factory transfers**, Chinese document printing (delivery note / picking list / work-order route card)
- **Integration & intelligence**: webhook event outlet (HMAC signature, retries, delivery log), AR/AP voucher export (CSV) for external finance systems, heuristic advisor (shortage / due-date / capacity)
- **PDA**: large-button touch pages for shop-floor reporting and warehouse ops (receiving / picking)

Details per release: [CHANGELOG.md](CHANGELOG.md). Quality baseline: golden path **49/49**, ledger reconciliation 0 diff, 200-concurrent reporting **P99 = 32ms**.

## Project Structure

```
evolver-mon/
├── backend/                 # Go+Gin API (cmd/server + internal/{api,crud,data,events,model,…} + seed)
├── frontend/                # React+TS+Vite frontend (src/pages by domain + generic CrudTable engine)
├── docs/                    # Functional design documents (see table below)
├── scripts/                 # Golden-path verification / concurrency load test
├── docker-compose.yml       # One-command deployment
└── LICENSE                  # Apache-2.0
```

## Functional Design Documents

> Note: PRD and design docs are written in Chinese; machine/browser translation works well for them.

| Document | Content |
|---|---|
| [PRD Master](docs/02-prd/00-prd-master.md) | Role matrix, cross-domain standards (multi-tenancy, document state machines, audit, permission numbering) |
| PRD Volumes | [Platform](docs/02-prd/01-prd-platform.md) · [ERP](docs/02-prd/02-prd-erp.md) · [MES](docs/02-prd/03-prd-mes.md) · [WMS](docs/02-prd/04-prd-wms.md) · [Non-functional](docs/02-prd/05-prd-nfr.md) |
| [Architecture](docs/03-design/architecture.md) | Layering, multi-tenancy, high-concurrency design, event outlet |
| [Database Design](docs/03-design/database-design.md) | Full DDL conventions, index & constraint list |
| [API Design](docs/03-design/api-design.md) | REST conventions, error codes, all endpoints |
| [Core Flows](docs/03-design/core-flows.md) | End-to-end flows, state machines, MRP algorithm, posting transaction |
| [CHANGELOG](CHANGELOG.md) | Feature list & quality baseline per milestone M1–M6 |

## Contributing

- Second-dev friendly: the generic CRUD engine makes a resource page ≈ 40 lines; the frontend `CrudTable` is symmetric; custom fields (jsonb `extra`) and webhooks provide extension points without touching the core.
- Quality gates: `go build ./... && go vet ./... && go test ./...`; frontend `npm run build` (strict tsc); golden-path regression script.
- Code of conduct: driven by real factory scenarios (PRD user stories); "clone competitor features" PRs are not accepted.

---

*EvolverMon — world-class open-source manufacturing software every factory can afford.*  
*Author: 码禾数字*
