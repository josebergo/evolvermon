// Package middleware：trace / cors / auth / rbac（架构文档 §2）。
package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/jwtx"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

// Trace 为每个请求生成 trace_id（NFR-050）。
func Trace() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("trace_id", uuid.NewString()[:8])
		c.Next()
	}
}

func CORS(allow string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", allow)
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization,Content-Type,Idempotency-Key")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// AuthCtx 认证后的请求上下文。
type AuthCtx struct {
	UserID    int64
	TenantID  int64
	FactoryID int64
	IsSuper   bool
	Username  string
	Perms     map[string]bool // 权限点集合
	DataScope model.DataScope
}

func GetAuth(c *gin.Context) *AuthCtx {
	if v, ok := c.Get("auth"); ok {
		return v.(*AuthCtx)
	}
	return nil
}

// Auth 解析 JWT 并加载权限（每次请求查库，规模上来后可加缓存）。
// 打印等新窗口场景支持 ?access_token= 查询参数兜底。
func Auth(jm *jwtx.Manager, db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr := ""
		if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
			tokenStr = strings.TrimPrefix(h, "Bearer ")
		} else if q := c.Query("access_token"); q != "" {
			tokenStr = q
		}
		if tokenStr == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, response.Body{Code: response.ErrAuth, Message: "missing bearer token"})
			return
		}
		cl, err := jm.Parse(tokenStr)
		if err != nil || cl.Kind != "access" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, response.Body{Code: response.ErrAuth, Message: "invalid token"})
			return
		}
		var user model.User
		if err := db.First(&user, cl.UserID).Error; err != nil || user.Status != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, response.Body{Code: response.ErrAuth, Message: "user disabled"})
			return
		}
		if user.TenantID != cl.TenantID {
			c.AbortWithStatusJSON(http.StatusUnauthorized, response.Body{Code: response.ErrAuth, Message: "tenant mismatch"})
			return
		}
		ac := &AuthCtx{
			UserID: user.ID, TenantID: user.TenantID, FactoryID: cl.FactoryID,
			IsSuper: user.IsSuper, Username: user.Username,
			Perms: map[string]bool{},
		}
		if ac.FactoryID == 0 {
			// 未选工厂时默认第一个可访问工厂
			var uf model.UserFactory
			if err := db.Where("user_id=?", user.ID).First(&uf).Error; err == nil {
				ac.FactoryID = uf.FactoryID
			}
		}
		if user.IsSuper {
			ac.Perms["*"] = true
			ac.DataScope = model.ScopeTenant
		} else {
			var roles []model.Role
			db.Joins("JOIN user_roles ur ON ur.role_id = roles.id AND ur.user_id = ?", user.ID).
				Where("roles.status = 1").Find(&roles)
			scope := model.ScopeSelf
			for _, r := range roles {
				if r.DataScope == model.ScopeTenant {
					scope = model.ScopeTenant
				} else if r.DataScope == model.ScopeFactory && scope != model.ScopeTenant {
					scope = model.ScopeFactory
				}
				for _, p := range strings.Split(r.PermCodes, ",") {
					if p = strings.TrimSpace(p); p != "" {
						ac.Perms[p] = true
					}
				}
			}
			ac.DataScope = scope
		}
		c.Set("auth", ac)
		c.Next()
	}
}

// Require(perm) RBAC 校验：超管或有对应权限点；perm 为空表示仅需登录。
func Require(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ac := GetAuth(c)
		if ac == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, response.Body{Code: response.ErrAuth, Message: "unauthenticated"})
			return
		}
		if perm == "" || ac.IsSuper || ac.Perms["*"] || ac.Perms[perm] {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, response.Body{Code: response.ErrForbidden, Message: "no permission: " + perm})
	}
}

// Logger 访问日志（结构化 stdout，NFR-050 字段）。
func Logger() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(p gin.LogFormatterParams) string {
		return fmt.Sprintf(`{"ts":%q,"method":%q,"path":%q,"status":%d,"latency_ms":%.1f,"ip":%q,"ua":%q}`+"\n",
			p.TimeStamp.Format(time.RFC3339), p.Method, p.Path, p.StatusCode,
			float64(p.Latency.Microseconds())/1000.0, p.ClientIP, p.Request.UserAgent())
	})
}
