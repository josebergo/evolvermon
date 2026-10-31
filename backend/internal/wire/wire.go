// Package wire 服务间通道接线（Phase 2 第二批）：各 main 启动时调用，把 identity
// 远程通道注入 data/middleware 包级钩子（避免 import 环：data、middleware 不依赖 client）。
// EM_IDENTITY_URL 未配置时为 no-op（Lite/identity 自身走本地直连，行为不变）。
package wire

import (
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/client"
	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
)

// Identity 启用 identity 远程通道（单号/审计/认证上下文）。幂等。
func Identity() {
	c := client.Identity()
	if c == nil {
		return
	}
	data.RemoteDocNo = c.DocNo
	data.RemoteAudit = c.Audit
	middleware.SetUserLoader(func(db *gorm.DB, userID int64) (*middleware.UserLoad, error) {
		dto, err := c.AuthContext(userID)
		if err != nil {
			return nil, err
		}
		perms := map[string]bool{}
		for _, p := range dto.Perms {
			perms[p] = true
		}
		return &middleware.UserLoad{
			TenantID: dto.TenantID, Username: dto.Username, FactoryID: dto.FactoryID,
			IsSuper: dto.IsSuper, Perms: perms, DataScope: model.DataScope(dto.DataScope),
		}, nil
	})
}
