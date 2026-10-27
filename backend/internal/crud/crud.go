// Package crud 通用 CRUD 引擎（架构 §3「通用 CRUD 引擎」，支撑 NFR-070 二开效率）。
// 用法：crud.Register[Factory](rg, db, crud.Options{...}) —— 列表/详情/创建/更新/删除 + 租户过滤 + 权限。
package crud

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/data"
	"github.com/evolvermon/evolvermon/internal/middleware"
	"github.com/evolvermon/evolvermon/internal/model"
	"github.com/evolvermon/evolvermon/internal/pkg/response"
)

// Holder 由 BaseModel 提升方法实现，使泛型能改写审计列。
type Holder interface{ GetBase() *model.BaseModel }

type Options[T any] struct {
	Path         string   // 路由前缀，如 /users
	FactoryScoped bool    // 业务表是否含 factory_id
	SearchCols   []string // ?q= 模糊搜索列（snake_case 列名）
	FilterCols   []string // 精确过滤列（snake_case 列名）
	DefaultSort  string   // 默认排序列
	Preload      []string
	// Hook 返回错误则整体失败
	BeforeCreate func(db *gorm.DB, ac *middleware.AuthCtx, obj *T) error
	AfterCreate  func(db *gorm.DB, ac *middleware.AuthCtx, obj *T) error
	BeforeUpdate func(db *gorm.DB, ac *middleware.AuthCtx, obj *T, old *T) error
	BeforeDelete func(db *gorm.DB, ac *middleware.AuthCtx, old *T) error
	DenyDelete   func(old *T) string // 返回非空则拒绝删除（如非草稿单据）
}

func Register[T any](rg gin.IRouter, db *gorm.DB, opt Options[T]) {
	permBase := strings.TrimSuffix(opt.Path, "s") // 粗略权限前缀，路由层仍显式 Require
	_ = permBase

	list := func(c *gin.Context) {
		ac := middleware.GetAuth(c)
		var objs []T
		// 查询构造器：Count 与 Find 各自构建，避免复用已终结的语句链
		build := func() *gorm.DB {
			q := db.Scopes(data.ScopeFor(ac, opt.FactoryScoped))
			for _, col := range opt.FilterCols {
				if v := c.Query(col); v != "" {
					q = q.Where(col+" = ?", v)
				}
			}
			if s := c.Query("q"); s != "" && len(opt.SearchCols) > 0 {
				var conds []string
				var args []any
				for _, col := range opt.SearchCols {
					conds = append(conds, col+" ILIKE ?")
					args = append(args, "%"+s+"%")
				}
				q = q.Where(strings.Join(conds, " OR "), args...)
			}
			if st := c.Query("status"); st != "" {
				q = q.Where("status = ?", st)
			}
			return q
		}
		var total int64
		build().Model(new(T)).Count(&total)
		page, size := pageParams(c)
		sort := c.DefaultQuery("sort", opt.DefaultSort)
		if sort == "" {
			sort = "id"
		}
		order := c.DefaultQuery("order", "desc")
		if order != "asc" {
			order = "desc"
		}
		q := build()
		for _, p := range opt.Preload {
			q = q.Preload(p)
		}
		if err := q.Order(sort + " " + order).Offset((page - 1) * size).Limit(size).Find(&objs).Error; err != nil {
			response.Fail(c, err)
			return
		}
		response.Page(c, objs, total)
	}

	get := func(c *gin.Context) {
		ac := middleware.GetAuth(c)
		var obj T
		q := db.Scopes(data.ScopeFor(ac, opt.FactoryScoped))
		for _, p := range opt.Preload {
			q = q.Preload(p)
		}
		if err := q.First(&obj, c.Param("id")).Error; err != nil {
			response.Fail(c, response.NewBiz(response.ErrParam, "记录不存在"))
			return
		}
		response.OK2(c, obj)
	}

	create := func(c *gin.Context) {
		ac := middleware.GetAuth(c)
		var obj T
		if err := c.ShouldBindJSON(&obj); err != nil {
			response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
			return
		}
		if h, ok := any(&obj).(Holder); ok {
			h.GetBase().TenantID = ac.TenantID
			h.GetBase().FactoryID = &ac.FactoryID
			h.GetBase().CreatedBy = ac.UserID
			h.GetBase().UpdatedBy = ac.UserID
		}
		if opt.BeforeCreate != nil {
			if err := opt.BeforeCreate(db, ac, &obj); err != nil {
				response.Fail(c, err)
				return
			}
		}
		if err := db.Create(&obj).Error; err != nil {
			response.Fail(c, response.NewBiz(response.ErrBiz, "创建失败："+err.Error()))
			return
		}
		if opt.AfterCreate != nil {
			if err := opt.AfterCreate(db, ac, &obj); err != nil {
				response.Fail(c, err)
				return
			}
		}
		response.OK2(c, obj)
	}

	update := func(c *gin.Context) {
		ac := middleware.GetAuth(c)
		var old, obj T
		if err := db.Scopes(data.ScopeFor(ac, opt.FactoryScoped)).First(&old, c.Param("id")).Error; err != nil {
			response.Fail(c, response.NewBiz(response.ErrParam, "记录不存在"))
			return
		}
		if err := c.ShouldBindJSON(&obj); err != nil {
			response.Fail(c, response.NewBiz(response.ErrParam, err.Error()))
			return
		}
		// 乐观锁：payload.version 必须等于当前 version
		var curVer, newVer int
		if h, ok := any(&old).(Holder); ok {
			curVer = h.GetBase().Version
		}
		if h, ok := any(&obj).(Holder); ok {
			newVer = h.GetBase().Version
			h.GetBase().Version = curVer + 1
			h.GetBase().TenantID = 0 // 不可改租户
			h.GetBase().FactoryID = nil
			h.GetBase().UpdatedBy = ac.UserID
			h.GetBase().CreatedBy = 0
			h.GetBase().CreatedAt = timeZero
		}
		if newVer != 0 && newVer != curVer {
			response.Fail(c, response.NewBiz(response.ErrVersion, "数据已被他人修改，请刷新"))
			return
		}
		if opt.BeforeUpdate != nil {
			if err := opt.BeforeUpdate(db, ac, &obj, &old); err != nil {
				response.Fail(c, err)
				return
			}
		}
		var pk int64
		if h, ok := any(&old).(Holder); ok {
			pk = h.GetBase().ID
		}
		if err := db.Model(new(T)).Where("id = ?", pk).Select("*").
			Omit("id", "tenant_id", "factory_id", "created_by", "created_at", "deleted_at", "password_hash", "doc_no").
			Updates(&obj).Error; err != nil {
			response.Fail(c, response.NewBiz(response.ErrBiz, "更新失败："+err.Error()))
			return
		}
		response.OK2(c, old)
	}

	remove := func(c *gin.Context) {
		ac := middleware.GetAuth(c)
		var old T
		if err := db.Scopes(data.ScopeFor(ac, opt.FactoryScoped)).First(&old, c.Param("id")).Error; err != nil {
			response.Fail(c, response.NewBiz(response.ErrParam, "记录不存在"))
			return
		}
		if opt.DenyDelete != nil {
			if reason := opt.DenyDelete(&old); reason != "" {
				response.Fail(c, response.NewBiz(response.ErrBiz, reason))
				return
			}
		}
		if opt.BeforeDelete != nil {
			if err := opt.BeforeDelete(db, ac, &old); err != nil {
				response.Fail(c, err)
				return
			}
		}
		if err := db.Delete(&old).Error; err != nil {
			response.Fail(c, response.NewBiz(response.ErrBiz, "删除失败："+err.Error()))
			return
		}
		response.OK2(c, gin.H{"deleted": true})
	}

	rg.GET(opt.Path, list)
	rg.GET(opt.Path+"/:id", get)
	rg.POST(opt.Path, create)
	rg.PUT(opt.Path+"/:id", update)
	rg.DELETE(opt.Path+"/:id", remove)
}

func pageParams(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	return page, size
}

var timeZero = time0()

func time0() (t time.Time) { return }
