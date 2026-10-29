// EvolverMon API Server 入口（cmd/server/main.go）。
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/evolvermon/evolvermon/internal/config"
	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/events"
	"github.com/evolvermon/evolvermon/internal/infra"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/jwtx"
	"github.com/evolvermon/evolvermon/internal/router"
	"github.com/evolvermon/evolvermon/seed"
)

func main() {
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
	version, err := data.MigrateVersioned(db)
	if err != nil {
		log.Fatalf("migrate failed: %v", err)
	}
	log.Printf("schema at version %d", version)
	if os.Getenv("EM_MIGRATE_ONLY") == "1" {
		return // 仅执行迁移（部署/CI 用）
	}
	if cfg.SeedDemo {
		if err := seed.Run(db); err != nil {
			log.Printf("seed failed (non-fatal): %v", err)
		}
	}

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

	events.StartWorker(db)
	r := router.New(db, jm)

	// 定时自动 MRP（FR-ERP-077 M2）：EM_MRP_AUTO_MINUTES>0 时启用，逐工厂运行并记录摘要
	if mins := config.MrpAutoMinutes(); mins > 0 {
		go func() {
			ticker := time.NewTicker(time.Duration(mins) * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				// 多副本部署时用分布式锁保证同一时刻只有一个实例跑 MRP
				release, ok, err := in.Locker.Acquire(context.Background(), "mrp-auto", time.Duration(mins)*time.Minute)
				if err != nil {
					log.Printf("[mrp-auto] lock: %v, skip this round", err)
					continue
				}
				if !ok {
					continue // 其他副本持有
				}
				var factories []model.Factory
				db.Where("status=1").Find(&factories)
				for _, f := range factories {
					if err := data.RunScheduledMrp(db, f.TenantID, f.ID); err != nil {
						log.Printf("[mrp-auto] factory %d: %v", f.ID, err)
					}
				}
				release()
			}
		}()
		log.Printf("scheduled MRP enabled: every %d minutes", config.MrpAutoMinutes())
	}

	port := cfg.HTTPAddr
	if p := os.Getenv("PORT"); p != "" {
		port = ":" + p
	}
	log.Printf("EvolverMon API listening on %s (env=%s)", port, cfg.Env)
	if err := r.Run(port); err != nil {
		log.Fatal(err)
	}
}
