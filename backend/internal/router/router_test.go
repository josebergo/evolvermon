package router

import (
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/pkg/jwtx"
)

// TestRouteRegistration 无数据库环境下的装配冒烟测试：
// 验证全部路由可注册（gin 路由树静态/参数冲突会在此 panic）。
func TestRouteRegistration(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: nil}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("init gorm without connection: %v", err)
	}
	r := New(db, jwtx.New("test", "evolvermon", 1, 1))
	if r == nil || len(r.Routes()) < 50 {
		t.Fatalf("routes registered: %d", len(r.Routes()))
	}
	t.Logf("registered %d routes", len(r.Routes()))
}
