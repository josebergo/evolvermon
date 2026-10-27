// Package api 业务接口层（M1 将 handler 与 service 合并，见架构文档修订说明）。
// 本文件：认证、上下文、菜单（FR-PLT-030~043）。
package api

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/jwtx"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

type AuthAPI struct {
	DB *gorm.DB
	JM *jwtx.Manager
}

type loginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (a *AuthAPI) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "用户名密码必填"))
		return
	}
	var user model.User
	if err := a.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrAuth, "用户名或密码错误"))
		return
	}
	if user.LockedUntil != nil && user.LockedUntil.After(time.Now()) {
		response.Fail(c, response.NewBiz(response.ErrAuth, "账号已锁定，请稍后再试"))
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		a.DB.Model(&user).UpdateColumn("fail_count", user.FailCount+1)
		if user.FailCount+1 >= 5 {
			until := time.Now().Add(15 * time.Minute)
			a.DB.Model(&model.User{}).Where("id=?", user.ID).Updates(map[string]any{"locked_until": until, "fail_count": 0})
		}
		response.Fail(c, response.NewBiz(response.ErrAuth, "用户名或密码错误"))
		return
	}
	if user.Status != 1 {
		response.Fail(c, response.NewBiz(response.ErrAuth, "账号已停用"))
		return
	}
	// FR-PLT-004：租户停用/过期后其用户拒绝登录
	var tenant model.Tenant
	if err := a.DB.First(&tenant, user.TenantID).Error; err == nil {
		if tenant.Status != 1 {
			response.Fail(c, response.NewBiz(response.ErrAuth, "所属租户已停用，请联系管理员"))
			return
		}
		if tenant.ExpiredAt != nil && tenant.ExpiredAt.Before(time.Now()) {
			response.Fail(c, response.NewBiz(response.ErrAuth, "所属租户已过期，请联系管理员"))
			return
		}
	}
	// 默认工厂
	var fid int64
	var uf model.UserFactory
	if err := a.DB.Where("user_id=? AND is_default=true", user.ID).First(&uf).Error; err == nil {
		fid = uf.FactoryID
	} else if err := a.DB.Where("user_id=?", user.ID).First(&uf).Error; err == nil {
		fid = uf.FactoryID
	}
	access, err1 := a.JM.IssueAccess(user.ID, user.TenantID, fid, user.IsSuper)
	refresh, err2 := a.JM.IssueRefresh(user.ID, user.TenantID, fid, user.IsSuper)
	if err1 != nil || err2 != nil {
		response.Fail(c, response.NewBiz(response.ErrSystem, "令牌签发失败"))
		return
	}
	now := time.Now()
	a.DB.Model(&model.User{}).Where("id=?", user.ID).Updates(map[string]any{"last_login_at": now, "fail_count": 0})
	data.Audit(a.DB, &middleware.AuthCtx{UserID: user.ID, TenantID: user.TenantID, Username: user.Username}, c.ClientIP(), "login", "user", i64s(user.ID), user.RealName, "", "")
	response.OK2(c, gin.H{
		"accessToken": access, "refreshToken": refresh,
		"user": gin.H{"id": user.ID, "username": user.Username, "realName": user.RealName, "isSuper": user.IsSuper},
		"factoryId": fid,
	})
}

type refreshReq struct {
	RefreshToken string `json:"refreshToken" binding:"required"`
}

func (a *AuthAPI) Refresh(c *gin.Context) {
	var req refreshReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "refreshToken 必填"))
		return
	}
	cl, err := a.JM.Parse(req.RefreshToken)
	if err != nil || cl.Kind != "refresh" {
		response.Fail(c, response.NewBiz(response.ErrAuth, "refreshToken 无效"))
		return
	}
	access, err := a.JM.IssueAccess(cl.UserID, cl.TenantID, cl.FactoryID, cl.IsSuper)
	if err != nil {
		response.Fail(c, response.NewBiz(response.ErrSystem, "令牌签发失败"))
		return
	}
	response.OK2(c, gin.H{"accessToken": access})
}

func (a *AuthAPI) Profile(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var user model.User
	if err := a.DB.First(&user, ac.UserID).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrAuth, "用户不存在"))
		return
	}
	// 可访问工厂
	var factories []model.Factory
	q := a.DB.Where("tenant_id=? AND status=1", ac.TenantID)
	if !ac.IsSuper && ac.DataScope == model.ScopeFactory {
		q = a.DB.Joins("JOIN user_factories uf ON uf.factory_id = factories.id AND uf.user_id=?", ac.UserID).
			Where("factories.tenant_id=? AND factories.status=1", ac.TenantID)
	}
	q.Find(&factories)
	// 权限点
	perms := []string{}
	for k := range ac.Perms {
		perms = append(perms, k)
	}
	// 菜单（按权限过滤）
	menus := a.menusFor(ac)
	response.OK2(c, gin.H{
		"user":       gin.H{"id": user.ID, "username": user.Username, "realName": user.RealName, "isSuper": user.IsSuper},
		"perms":      perms,
		"menus":      menus,
		"factories":  factories,
		"factoryId":  ac.FactoryID,
		"dataScope":  ac.DataScope,
	})
}

type switchReq struct {
	FactoryID int64 `json:"factoryId" binding:"required"`
}

// SwitchFactory FR-PLT-033：切换工厂后重新签发令牌。
func (a *AuthAPI) SwitchFactory(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var req switchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "factoryId 必填"))
		return
	}
	var f model.Factory
	if err := a.DB.Where("tenant_id=? AND id=? AND status=1", ac.TenantID, req.FactoryID).First(&f).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "无权访问该工厂"))
		return
	}
	if !ac.IsSuper {
		var cnt int64
		a.DB.Model(&model.UserFactory{}).Where("user_id=? AND factory_id=?", ac.UserID, req.FactoryID).Count(&cnt)
		if cnt == 0 {
			response.Fail(c, response.NewBiz(response.ErrForbidden, "您不属于该工厂"))
			return
		}
	}
	access, err := a.JM.IssueAccess(ac.UserID, ac.TenantID, req.FactoryID, ac.IsSuper)
	if err != nil {
		response.Fail(c, response.NewBiz(response.ErrSystem, "令牌签发失败"))
		return
	}
	data.Audit(a.DB, ac, c.ClientIP(), "switch-factory", "factory", i64s(req.FactoryID), f.Name, "", "")
	response.OK2(c, gin.H{"accessToken": access, "factoryId": req.FactoryID})
}

type pwdReq struct {
	Old string `json:"old" binding:"required"`
	New string `json:"new" binding:"required,min=6"`
}

func (a *AuthAPI) ChangePassword(c *gin.Context) {
	ac := middleware.GetAuth(c)
	var req pwdReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "新密码至少 6 位"))
		return
	}
	var user model.User
	if err := a.DB.First(&user, ac.UserID).Error; err != nil {
		response.Fail(c, response.NewBiz(response.ErrAuth, "用户不存在"))
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Old)) != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, "旧密码错误"))
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.New), bcrypt.DefaultCost)
	a.DB.Model(&model.User{}).Where("id=?", user.ID).Update("password_hash", string(hash))
	data.Audit(a.DB, ac, c.ClientIP(), "change-password", "user", i64s(user.ID), user.Username, "", "")
	response.OK2(c, gin.H{"ok": true})
}

// menusFor 菜单树（FR-PLT-040）：目录/页面按权限点过滤，无权限点的页面菜单默认可见（页面内按钮控制）。
func (a *AuthAPI) menusFor(ac *middleware.AuthCtx) []gin.H {
	var all []model.Menu
	a.DB.Order("sort").Find(&all)
	allowed := map[int64]bool{}
	for _, m := range all {
		if m.PermCode == "" || ac.Perms["*"] || ac.Perms[m.PermCode] {
			allowed[m.ID] = true
		}
	}
	// 父目录无权限子项则不显示：自底向上保留有权限节点的祖先
	keep := map[int64]bool{}
	byID := map[int64]*model.Menu{}
	for i := range all {
		byID[all[i].ID] = &all[i]
	}
	var markAncestors func(id int64)
	markAncestors = func(id int64) {
		if keep[id] {
			return
		}
		keep[id] = true
		if m, ok := byID[id]; ok && m.ParentID != 0 {
			markAncestors(m.ParentID)
		}
	}
	for id, ok := range allowed {
		if ok {
			markAncestors(id)
		}
	}
	var build func(pid int64) []gin.H
	build = func(pid int64) []gin.H {
		var out []gin.H
		for i := range all {
			m := all[i]
			if m.ParentID == pid && keep[m.ID] && m.Visible {
				out = append(out, gin.H{
					"id": m.ID, "parentId": m.ParentID, "name": m.Name, "path": m.Path,
					"icon": m.Icon, "sort": m.Sort, "type": m.Type, "permCode": m.PermCode,
					"children": build(m.ID),
				})
			}
		}
		return out
	}
	return build(0)
}

func i64s(v int64) string { return strconv.FormatInt(v, 10) }
