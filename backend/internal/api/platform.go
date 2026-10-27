package api

// 平台扩展：角色授权、用户角色、参数、审计查询、工作台/看板（FR-PLT-050~063, FR-MES-050~051）。

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
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

// ---- A1 租户管理（FR-PLT-001~006，仅超管） ----

func (p *PlatformAPI) ListTenants(c *gin.Context) {
	var tenants []model.Tenant
	q := p.DB.Model(&model.Tenant{})
	if v := c.Query("q"); v != "" {
		q = q.Where("name ILIKE ? OR code ILIKE ?", "%"+v+"%", "%"+v+"%")
	}
	var total int64
	q.Count(&total)
	q.Order("id DESC").Limit(100).Find(&tenants)
	response.Page(c, tenants, total)
}

func (p *PlatformAPI) CreateTenant(c *gin.Context) {
	ac := middleware.GetAuth(c)
	if !ac.IsSuper {
		response.Fail(c, response.NewBiz(response.ErrForbidden, "仅超级管理员可创建租户"))
		return
	}
	var t model.Tenant
	if err := c.ShouldBindJSON(&t); err != nil || t.Code == "" || t.Name == "" {
		response.Fail(c, response.NewBiz(response.ErrParam, "code/name 必填"))
		return
	}
	t.Status = 1
	if err := p.DB.Create(&t).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrBiz, "创建失败（编码重复？）："+err.Error()))
		return
	}
	// FR-PLT-003：租户级参数模板初始化
	p.DB.Create(&model.DocNumberRule{TenantID: t.ID, DocType: "SO", Prefix: "SO", SeqLen: 4})
	p.DB.Create(&model.SysParam{TenantID: t.ID, Key: "allow_negative_stock", Value: `"false"`, Remark: "允许负库存"})
	data.Audit(p.DB, ac, c.ClientIP(), "create", "tenant", t.Code, t.Name, "", "active")
	response.OK2(c, t)
}

// UpdateTenant 停用/启用/编辑（FR-PLT-004：停用后其用户拒绝登录）。
func (p *PlatformAPI) UpdateTenant(c *gin.Context) {
	ac := middleware.GetAuth(c)
	if !ac.IsSuper {
		response.Fail(c, response.NewBiz(response.ErrForbidden, "仅超级管理员可管理租户"))
		return
	}
	var t model.Tenant
	if err := p.DB.First(&t, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "租户不存在"))
		return
	}
	var req struct {
		Name         string     `json:"name"`
		Status       *int16     `json:"status"`
		ExpiredAt    *time.Time `json:"expiredAt"`
		ContactName  string     `json:"contactName"`
		ContactPhone string     `json:"contactPhone"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	ups := map[string]any{}
	if req.Name != "" {
		ups["name"] = req.Name
	}
	if req.Status != nil {
		ups["status"] = *req.Status
	}
	if req.ExpiredAt != nil {
		ups["expired_at"] = req.ExpiredAt
	}
	if req.ContactName != "" {
		ups["contact_name"] = req.ContactName
	}
	if req.ContactPhone != "" {
		ups["contact_phone"] = req.ContactPhone
	}
	if len(ups) > 0 {
		p.DB.Model(&model.Tenant{}).Where("id=?", t.ID).Updates(ups)
	}
	data.Audit(p.DB, ac, c.ClientIP(), "update", "tenant", t.Code, t.Name, "", fmt.Sprintf("%v", ups))
	response.OK2(c, gin.H{"ok": true})
}

// ---- A2 单号规则配置（FR-PLT-061） ----

func (p *PlatformAPI) ListDocRules(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var rules []model.DocNumberRule
	p.DB.Where("tenant_id=?", ac.TenantID).Order("doc_type").Find(&rules)
	response.OK2(c, rules)
}

func (p *PlatformAPI) UpdateDocRule(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var rule model.DocNumberRule
	if err := p.DB.Where("tenant_id=?", ac.TenantID).First(&rule, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "规则不存在"))
		return
	}
	var req struct {
		Prefix         string `json:"prefix"`
		SeqLen         int    `json:"seqLen"`
		HasFactoryCode *bool  `json:"hasFactoryCode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	ups := map[string]any{}
	if req.Prefix != "" {
		ups["prefix"] = req.Prefix
	}
	if req.SeqLen >= 3 && req.SeqLen <= 8 {
		ups["seq_len"] = req.SeqLen
	}
	if req.HasFactoryCode != nil {
		ups["has_factory_code"] = *req.HasFactoryCode
	}
	if len(ups) > 0 {
		p.DB.Model(&model.DocNumberRule{}).Where("id=?", rule.ID).Updates(ups)
	}
	data.Audit(p.DB, ac, c.ClientIP(), "update", "doc_number_rule", rule.DocType, rule.DocType, rule.Prefix, fmt.Sprintf("%v", ups))
	response.OK2(c, gin.H{"ok": true})
}

// ---- A3 管理员重置密码（FR-PLT-023） ----

func (p *PlatformAPI) ResetPassword(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var req struct {
		NewPassword string `json:"newPassword" binding:"required,min=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "新密码至少 6 位"))
		return
	}
	var u model.User
	if err := p.DB.Scopes(data.ScopeFor(ac, false)).First(&u, c.Param("id")).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "用户不存在"))
		return
	}
	if u.IsSuper && !ac.IsSuper {
		response.Fail(c, response.NewBiz(response.ErrForbidden, "仅超管可重置超管密码"))
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	p.DB.Model(&model.User{}).Where("id=?", u.ID).Update("password_hash", string(hash))
	data.Audit(p.DB, ac, c.ClientIP(), "reset-password", "user", u.Username, u.Username, "", "")
	response.OK2(c, gin.H{"ok": true})
}
