// Package scheduler 定时任务域（Phase 1.3）：定时 MRP 循环，供 cmd/server（Lite 聚合）
// 与 cmd/scheduler（Micro 独立 worker）共用；多副本经分布式锁 mrp-auto 互斥。
package scheduler

import (
	"context"
	"log"
	"time"

	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/infra"
	"github.com/evolvermon/evolvermon/internal/model"
)

// StartMrpTicker 定时自动 MRP（FR-ERP-077）：每 minutes 分钟逐工厂运行，阻塞直至 stop 关闭。
func StartMrpTicker(db *gorm.DB, locker infra.Locker, minutes int, stop <-chan struct{}) {
	ticker := time.NewTicker(time.Duration(minutes) * time.Minute)
	defer ticker.Stop()
	log.Printf("[mrp-auto] enabled: every %d minutes", minutes)
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			runOnce(db, locker, minutes)
		}
	}
}

func runOnce(db *gorm.DB, locker infra.Locker, minutes int) {
	// 多副本部署时用分布式锁保证同一时刻只有一个实例跑 MRP
	release, ok, err := locker.Acquire(context.Background(), "mrp-auto", time.Duration(minutes)*time.Minute)
	if err != nil {
		log.Printf("[mrp-auto] lock: %v, skip this round", err)
		return
	}
	if !ok {
		return // 其他副本持有
	}
	defer release()
	var factories []model.Factory
	db.Where("status=1").Find(&factories)
	for _, f := range factories {
		if err := data.RunScheduledMrp(db, f.TenantID, f.ID); err != nil {
			log.Printf("[mrp-auto] factory %d: %v", f.ID, err)
		}
	}
}
