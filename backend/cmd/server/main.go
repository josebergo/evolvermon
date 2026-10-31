// EvolverMon API Server 入口（cmd/server）。
// Lite 形态（默认 EM_ROLES=all）：单进程聚合全部域 + webhook worker + 定时 MRP。
// Micro 形态：EM_ROLES=core 时仅核心交易域（master/erp/mes/wms），platform/integration
// 由 cmd/identity、cmd/integration 独立进程承载，定时 MRP 由 cmd/scheduler 承载。
package main

import (
	"log"
	"os"
	"strings"
	"time"

	"github.com/evolvermon/evolvermon/internal/client"
	"github.com/evolvermon/evolvermon/internal/wire"
	ciapi "github.com/evolvermon/evolvermon/internal/core/internalapi"
	"github.com/evolvermon/evolvermon/internal/config"
	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/events"
	"github.com/evolvermon/evolvermon/internal/infra"
	"github.com/evolvermon/evolvermon/internal/pkg/jwtx"
	"github.com/evolvermon/evolvermon/internal/outbox"
	"github.com/evolvermon/evolvermon/internal/pkg/logx"
	"github.com/evolvermon/evolvermon/internal/router"
	"github.com/evolvermon/evolvermon/internal/scheduler"
	"github.com/evolvermon/evolvermon/seed"
)

func getenvDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	roles := getenvDefault("EM_ROLES", "all")
	// Phase 0.4：结构化日志（Lite 形态 service=evolvermon；Micro 各 cmd/<svc> 各自命名）
	logx.Init(getenvDefault("EM_SERVICE", map[string]string{"all": "evolvermon", "core": "core"}[roles]))
	wire.Identity() // Phase 2：identity 远程通道（单号/审计/认证上下文；未设 EM_IDENTITY_URL 时 no-op）

	cfg := config.Load()

	db, err := data.Open(cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("connect database failed: %v", err)
	}
	for i := 1; i <= 10; i++ { // 容器启动竞态：等 PG 就绪
		if err := db.Exec("SELECT 1").Error; err == nil {
			break
		}
		log.Printf("waiting for database (%d/10)...", i)
		time.Sleep(3 * time.Second)
	}
	if os.Getenv("EM_MIGRATE") != "0" {
		migProfile := "all"
	if roles == "core" {
		migProfile = "core"
	}
	version, err := data.MigrateVersioned(db, migProfile)
		if err != nil {
			log.Fatalf("migrate failed: %v", err)
		}
		log.Printf("schema at version %d", version)
		if os.Getenv("EM_MIGRATE_ONLY") == "1" {
			return // 仅执行迁移（部署/CI 用）
		}
	} else {
		log.Printf("migrate skipped (EM_MIGRATE=0)")
	}
	if cfg.SeedDemo {
		if roles == "all" || roles == "platform" {
			if err := seed.Run(db); err != nil {
				log.Printf("seed failed (non-fatal): %v", err)
			}
		}
		if roles == "core" {
			if err := seed.RunCoreDemo(db); err != nil { // 分库形态：core 库演示主数据
				log.Printf("seed core demo failed (non-fatal): %v", err)
			}
		}
	}

	client.InitDefaultMaster(db) // 主数据读通道默认装配（Lite=Local 直连）

	jm := jwtx.New(cfg.JWTSecret, cfg.JWTIssuer, cfg.AccessTTLHour, cfg.RefreshTTLDay)

	// 基础设施端口层：默认全 memory（单机零依赖）；水平扩展时切换
	// EM_BROKER=rabbitmq|kafka、EM_CACHE=redis（见 docs/03-design/infra-ports.md）
	in, err := infra.Open(infra.Options{
		Broker: cfg.Broker, BrokerURL: cfg.BrokerURL,
		Cache: cfg.Cache, CacheURL: cfg.CacheURL,
	})
	if err != nil {
		log.Fatalf("infra open failed: %v", err)
	}
	log.Printf("infra ready: broker=%s cache=%s(locker=%s)", in.Broker.Name(), in.Cache.Name(), in.Locker.Name())
	events.UseBroker(in.Broker)
	client.UseCache(in.Cache) // Phase 2.3：跨服务读通道缓存端口

	// Micro 分工：webhook 投递 worker 只在聚合/集成进程跑；定时 MRP 在聚合/scheduler 进程跑
	if roles == "all" {
		events.StartWorker(db)
		outbox.StartRelay(db, in.Broker) // Lite：聚合进程内闭环
	}
	if roles == "core" {
		events.UseRemoteFanout(true)     // 分库：webhook 扇出由 integration 拉取
		outbox.StartRelay(db, in.Broker) // core outbox relay 在 core 库
	}
	r := router.New(db, jm, routerDomains(roles)...)
	if roles == "core" || roles == "all" {
		(&ciapi.API{DB: db}).Register(r) // /internal/core/*：events 拉取 + factory-check
	}

	// 定时自动 MRP（FR-ERP-077 M2）：EM_MRP_AUTO_MINUTES>0 时启用，逐工厂运行并记录摘要
	if mins := config.MrpAutoMinutes(); mins > 0 && (roles == "all" || roles == "scheduler") {
		go scheduler.StartMrpTicker(db, in.Locker, mins, nil)
	}

	port := cfg.HTTPAddr
	if p := os.Getenv("PORT"); p != "" {
		port = ":" + p
	}
	log.Printf("EvolverMon API (%s) listening on %s (env=%s)", roles, port, cfg.Env)
	if err := r.Run(port); err != nil {
		log.Fatal(err)
	}
}

// routerDomains EM_ROLES → 路由域（空 = 全部注册，Lite 行为不变）。
func routerDomains(roles string) []string {
	switch strings.TrimSpace(roles) {
	case "all":
		return nil
	case "core":
		return []string{"core"}
	case "platform":
		return []string{"platform"}
	case "integration":
		return []string{"integration"}
	default:
		log.Fatalf("invalid EM_ROLES: %s (all|core|platform|integration)", roles)
		return nil
	}
}
