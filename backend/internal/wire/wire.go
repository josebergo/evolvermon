// Package wire 服务间通道接线（Phase 2 第二批）：各 main 启动时调用，把 identity
// 远程通道注入 data/middleware 包级钩子（避免 import 环：data、middleware 不依赖 client）。
// EM_IDENTITY_URL 未配置时为 no-op（Lite/identity 自身走本地直连，行为不变）。
package wire

import (
	"gorm.io/gorm"

	"evolvermon/backend/internal/client"
	"evolvermon/backend/internal/wms/posting"
	"evolvermon/backend/internal/wms/wapi"
	"evolvermon/backend/internal/data"
	"evolvermon/backend/internal/middleware"
	"evolvermon/backend/internal/model"
)

// Identity 启用 identity 远程通道（单号/审计/认证上下文）。幂等。
func Identity() {
	wapi.DocNo = data.NextDocNo
	posting.RemoteWriteBack = posting.HTTPWriteBack // wms 过账跨域回写（Remote 拆库形态）
	client.SetWODocNo(func(db *gorm.DB, tenantID int64) (string, error) { return data.NextDocNo(db, tenantID, "WO") }) // wapi 建单取号（data.NextDocNo 内部已含远程钩子；Lite=本地）
	c := client.Identity()
	if c == nil {
		return
	}
	data.RemoteDocNo = c.DocNo
	data.RemoteAudit = c.Audit
	data.RemoteParamPut = c.PutParam
	data.RemoteBoolParam = c.BoolParam
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
