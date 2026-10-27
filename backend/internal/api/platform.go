package api

// 平台扩展：角色授权、用户角色、参数、审计查询、工作台/看板（FR-PLT-050~063, FR-MES-050~051）。

import (
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

type PlatformAPI struct{ DB *gorm.DB }

// UpdateRolePerms FR-PLT-021。
func (p *PlatformAPI) UpdateRolePerms(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var req struct {
		PermCodes []string `json:"permCodes"`
		DataScope string   `json:"dataScope"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	var role model.Role
	if err := p.DB.Scopes(data.ScopeFor(ac, false)).First(&role, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "角色不存在"))
		return
	}
	joined := ""
	for i, pc := range req.PermCodes {
		if i > 0 {
			joined += ","
		}
		joined += pc
	}
	updates := map[string]any{"perm_codes": joined}
	if req.DataScope != "" {
		updates["data_scope"] = req.DataScope
	}
	p.DB.Model(&model.Role{}).Where("id=?", role.ID).Updates(updates)
	data.Audit(p.DB, ac, c.ClientIP(), "update-perms", "role", role.Code, role.Name, role.PermCodes, joined)
	response.OK2(c, gin.H{"ok": true})
}

// UpdateUserRoles FR-PLT-022。
func (p *PlatformAPI) UpdateUserRoles(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var req struct {
		RoleIDs    []int64 `json:"roleIds"`
		FactoryIDs []int64 `json:"factoryIds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	var user model.User
	if err := p.DB.Scopes(data.ScopeFor(ac, false)).First(&user, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "用户不存在"))
		return
	}
	err := p.DB.Transaction(func(tx *gorm.DB) error {
		if req.RoleIDs != nil {
			if err := tx.Where("user_id=?", user.ID).Delete(&model.UserRole{}).Error; err != nil {
				return err
			}
			for _, rid := range req.RoleIDs {
				if err := tx.Create(&model.UserRole{UserID: user.ID, RoleID: rid}).Error; err != nil {
					return err
				}
			}
		}
		if req.FactoryIDs != nil {
			if err := tx.Where("user_id=?", user.ID).Delete(&model.UserFactory{}).Error; err != nil {
				return err
			}
			for i, fid := range req.FactoryIDs {
				if err := tx.Create(&model.UserFactory{UserID: user.ID, FactoryID: fid, IsDefault: i == 0}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"ok": true})
}

// Params 租户参数读/写（FR-PLT-060）。
func (p *PlatformAPI) GetParams(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var list []model.SysParam
	p.DB.Where("tenant_id=? AND factory_id IS NULL", ac.TenantID).Find(&list)
	response.OK2(c, list)
}

func (p *PlatformAPI) PutParam(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var req model.SysParam
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	var existing model.SysParam
	err := p.DB.Where("tenant_id=? AND factory_id IS NULL AND key=?", ac.TenantID, req.Key).First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		req.TenantID = ac.TenantID
		req.FactoryID = nil
		req.UpdatedAt = time.Now()
		p.DB.Create(&req)
	} else if err == nil {
		p.DB.Model(&model.SysParam{}).Where("id=?", existing.ID).Updates(map[string]any{"value": req.Value, "remark": req.Remark, "updated_at": time.Now()})
	} else {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"ok": true})
}

// AuditLogs FR-PLT-051 审计查询。
func (p *PlatformAPI) AuditLogs(c *gin.Context) {
	ac := middleware.GetAuth(c)
	q := p.DB.Model(&model.AuditLog{}).Where("tenant_id=?", ac.TenantID)
	if v := c.Query("action"); v != "" {
		q = q.Where("action=?", v)
	}
	if v := c.Query("username"); v != "" {
		q = q.Where("username ILIKE ?", "%"+v+"%")
	}
	var list []model.AuditLog
	var total int64
	q.Count(&total)
	q.Order("id DESC").Limit(500).Find(&list)
	response.Page(c, list, total)
}

// Perms 权限点清单（前端角色编辑用，来自菜单表 perm_code）。
func (p *PlatformAPI) PermList(c *gin.Context) {
	var menus []model.Menu
	p.DB.Where("perm_code <> ''").Order("sort").Find(&menus)
	type perm struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	var out []perm
	for _, m := range menus {
		out = append(out, perm{Code: m.PermCode, Name: m.Name + "（" + m.PermCode + "）"})
	}
	response.OK2(c, out)
}
