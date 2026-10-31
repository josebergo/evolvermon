// Package internalapi identity 服务内部 API（Phase 2 第二批，附录 C 销号）：拆 identity 库的前置——
// core/master 等服务经此取「认证上下文 / 单号 / 审计落库」，不再直连 platform 表。
package internalapi

import (
	"net/http"
	"strconv"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

type API struct{ DB *gorm.DB }

// Register 挂载 /internal/identity/*（仅服务间调用，网关不路由）。
func (a *API) Register(r *gin.Engine) {
	g := r.Group("/internal/identity", internalToken())
	g.GET("/auth-context", a.AuthContext)
	g.POST("/docno", a.DocNo)
	g.POST("/audit", a.Audit)
	g.GET("/factories", a.Factories)
	g.POST("/param", a.Param)
	g.GET("/param", a.GetParam)
}

func internalToken() gin.HandlerFunc {
	token := os.Getenv("EM_INTERNAL_TOKEN")
	return func(c *gin.Context) {
		if token == "" || c.GetHeader("X-Internal-Token") != token {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "bad internal token"})
			return
		}
		c.Next()
	}
}

// AuthContext 用户认证上下文（user 状态/默认工厂/角色权限/数据范围）——middleware.Auth 远程加载源。
func (a *API) AuthContext(c *gin.Context) {
	userID := int64(0)
	for _, ch := range c.Query("userId") {
		if ch < '0' || ch > '9' {
			response.Fail(c, response.NewBiz(response.ErrParam, "bad userId"))
			return
		}
		userID = userID*10 + int64(ch-'0')
	}
	var user model.User
	if err := a.DB.First(&user, userID).Error; err != nil || user.Status != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "user disabled"})
		return
	}
	var uf model.UserFactory
	a.DB.Where("user_id=?", userID).Order("is_default DESC").First(&uf)
	perms := []string{}
	isSuper := user.IsSuper
	scope := model.ScopeSelf
	if isSuper {
		perms = []string{"*"}
		scope = model.ScopeTenant
	} else {
		var roles []model.Role
		a.DB.Joins("JOIN platform.user_roles ur ON ur.role_id = roles.id AND ur.user_id = ?", userID).
			Where("roles.status = 1").Find(&roles)
		seen := map[string]bool{}
		for _, r := range roles {
			if r.DataScope == model.ScopeTenant {
				scope = model.ScopeTenant
			} else if r.DataScope == model.ScopeFactory && scope != model.ScopeTenant {
				scope = model.ScopeFactory
			}
			for _, p := range strings.Split(r.PermCodes, ",") {
				if p = strings.TrimSpace(p); p != "" && !seen[p] {
					seen[p] = true
					perms = append(perms, p)
				}
			}
		}
	}
	response.OK2(c, gin.H{
		"userId": user.ID, "tenantId": user.TenantID, "username": user.Username,
		"isSuper": isSuper, "factoryId": uf.FactoryID, "perms": perms, "dataScope": string(scope),
	})
}

// DocNo 取单号（服务端行锁逻辑与 data.NextDocNo 一致）。
func (a *API) DocNo(c *gin.Context) {
	var req struct {
		TenantID int64  `json:"tenantId"`
		DocType  string `json:"docType"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.TenantID == 0 || req.DocType == "" {
		response.Fail(c, response.NewBiz(response.ErrParam, "tenantId/docType required"))
		return
	}
	docNo, err := data.NextDocNo(a.DB, req.TenantID, req.DocType)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"docNo": docNo})
}

// Audit 审计落库（core 各域 → identity；尽力而为）。
func (a *API) Audit(c *gin.Context) {
	var req struct {
		TenantID int64  `json:"tenantId"`
		UserID   int64  `json:"userId"`
		Username string `json:"username"`
		IP       string `json:"ip"`
		Action   string `json:"action"`
		ObjType  string `json:"objType"`
		ObjID    string `json:"objId"`
		ObjDesc  string `json:"objDesc"`
		Before   string `json:"before"`
		After    string `json:"after"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
		return
	}
	err := a.DB.Create(&model.AuditLog{
		TenantID: req.TenantID, UserID: req.UserID, Username: req.Username, IP: req.IP,
		Action: req.Action, ObjectType: req.ObjType, ObjectID: req.ObjID, ObjectDesc: req.ObjDesc,
		Before: req.Before, After: req.After, CreatedAt: time.Now(),
	}).Error
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"ok": true})
}

// Factories 活跃工厂列表（scheduler 定时 MRP / core 调拨读取）。
func (a *API) Factories(c *gin.Context) {
	var rows []model.Factory
	a.DB.Select("id, tenant_id, code, name, status").Where("status=1").Find(&rows)
	response.OK2(c, gin.H{"list": rows})
}

// GetParam 读参数（core 过账负库存开关等；返回 value 字符串）。
func (a *API) GetParam(c *gin.Context) {
	tenantID, _ := strconv.ParseInt(c.Query("tenantId"), 10, 64)
	var p model.SysParam
	if err := a.DB.Where("tenant_id=? AND factory_id IS NULL AND key=?", tenantID, c.Query("key")).Order("id DESC").First(&p).Error; err != nil {
		response.OK2(c, gin.H{"value": ""}) // 未配置=默认 false
		return
	}
	response.OK2(c, gin.H{"value": p.Value})
}

// Param 写系统参数（scheduler MRP 摘要等；UPSERT）。
func (a *API) Param(c *gin.Context) {
	var req struct {
		TenantID  int64  `json:"tenantId"`
		FactoryID int64  `json:"factoryId"`
		Key       string `json:"key"`
		Value     string `json:"value"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.TenantID == 0 || req.Key == "" {
		response.Fail(c, response.NewBiz(response.ErrParam, "tenantId/key required"))
		return
	}
	err := a.DB.Exec(`INSERT INTO platform.sys_params(tenant_id, factory_id, key, value, updated_at)
		VALUES(?,?,?,?,'now()')
		ON CONFLICT (tenant_id, factory_id, key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()`,
		req.TenantID, req.FactoryID, req.Key, req.Value).Error
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK2(c, gin.H{"ok": true})
}

var _ = middleware.GetAuth // 保持 middleware 引用（AuthCtx 语义对齐）
var _ = clause.Locking{}
