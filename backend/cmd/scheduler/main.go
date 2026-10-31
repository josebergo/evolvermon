// cmd/scheduler：定时任务 worker（Phase 1.3，Micro 形态，无业务 HTTP）。
// 定时 MRP（分布式锁 mrp-auto 互斥，多副本安全）+ 健康探针/指标（供 ops-admin）。
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/evolvermon/evolvermon/internal/config"
	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/infra"
	"github.com/evolvermon/evolvermon/internal/pkg/logx"
	"github.com/evolvermon/evolvermon/internal/pkg/metrics"
	"github.com/evolvermon/evolvermon/internal/scheduler"
)

func main() {
	logx.Init("scheduler")
	cfg := config.Load()

	db, err := data.Open(cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("connect database failed: %v", err)
	}
	for i := 1; i <= 10; i++ {
		if err := db.Exec("SELECT 1").Error; err == nil {
			break
		}
		log.Printf("waiting for database (%d/10)...", i)
		time.Sleep(3 * time.Second)
	}
	if os.Getenv("EM_MIGRATE") != "0" {
		if _, err := data.MigrateVersioned(db); err != nil {
			log.Fatalf("migrate failed: %v", err)
		}
	} else {
		log.Printf("migrate skipped (EM_MIGRATE=0)")
	}

	in, err := infra.Open(infra.Options{Broker: cfg.Broker, BrokerURL: cfg.BrokerURL, Cache: cfg.Cache, CacheURL: cfg.CacheURL})
	if err != nil {
		log.Fatalf("infra open failed: %v", err)
	}

	mins := config.MrpAutoMinutes()
	stop := make(chan struct{})
	if mins > 0 {
		go scheduler.StartMrpTicker(db, in.Locker, mins, stop)
	} else {
		log.Printf("[mrp-auto] disabled (EM_MRP_AUTO_MINUTES<=0)")
	}

	// 健康探针/指标（ops-admin 抓取）
	addr := getenvDefault("EM_HTTP_ADDR", ":8084")
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{"status":"ok"}`)) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Exec("SELECT 1").Error; err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"db-down"}`))
			return
		}
		w.Write([]byte(`{"status":"ready"}`))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(metrics.Default().Render()))
	})
	log.Printf("scheduler listening on %s (mrp=%dmin)", addr, mins)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func getenvDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
