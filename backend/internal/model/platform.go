package model

import "time"

// ---------------- 平台域（数据库设计 §1） ----------------

type Tenant struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	Code         string    `gorm:"uniqueIndex;size:32" json:"code"`
	Name         string    `gorm:"size:128" json:"name"`
	ContactName  string    `gorm:"size:64" json:"contactName"`
	ContactPhone string    `gorm:"size:32" json:"contactPhone"`
	Status       int16     `gorm:"default:1" json:"status"` // 1 启用 0 停用
	ExpiredAt    *time.Time `json:"expiredAt"`
	Config       string    `gorm:"type:jsonb;default:'{}'" json:"config"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type Factory struct {
	BaseModel
	Code         string `gorm:"uniqueIndex:uq_factory;size:32" json:"code"`
	Name         string `gorm:"size:128" json:"name"`
	Address      string `gorm:"size:256" json:"address"`
	ManagerUser  string `gorm:"size:64" json:"managerUser"`
	Status       int16  `gorm:"default:1" json:"status"`
}

type WarehouseType string

const (
	WhRaw    WarehouseType = "raw"        // 原料仓
	WhFG     WarehouseType = "finished"   // 成品仓
	WhSemi   WarehouseType = "semi"       // 半成品仓
	WhDefect WarehouseType = "defective"  // 不良品仓
	WhTransit WarehouseType = "transit"   // 在途仓
)

type Warehouse struct {
	BaseModel
	Code   string         `gorm:"uniqueIndex:uq_wh;size:32" json:"code"`
	Name   string         `gorm:"size:128" json:"name"`
	Type   WarehouseType  `gorm:"size:16;default:'raw'" json:"type"`
	Status int16          `gorm:"default:1" json:"status"`
}

type Location struct {
	BaseModel
	WarehouseID int64  `gorm:"index" json:"warehouseId"`
	Code        string `gorm:"uniqueIndex:uq_loc;size:64" json:"code"`
	Zone        string `gorm:"size:32" json:"zone"`
	Rack        string `gorm:"size:32" json:"rack"`
	Frozen      bool   `gorm:"default:false" json:"frozen"`
	Status      int16  `gorm:"default:1" json:"status"`
}

type User struct {
	BaseModel
	Username    string     `gorm:"uniqueIndex:uq_user;size:64" json:"username"`
	PasswordHash string    `json:"-"`
	RealName    string     `gorm:"size:64" json:"realName"`
	Mobile      string     `gorm:"size:32" json:"mobile"`
	Email       string     `gorm:"size:128" json:"email"`
	IsSuper     bool       `gorm:"default:false" json:"isSuper"`
	Status      int16      `gorm:"default:1" json:"status"`
	FailCount   int        `gorm:"default:0" json:"-"`
	LockedUntil *time.Time `json:"-"`
	LastLoginAt *time.Time `json:"lastLoginAt"`
}

type UserFactory struct {
	UserID    int64 `gorm:"primaryKey" json:"userId"`
	FactoryID int64 `gorm:"primaryKey" json:"factoryId"`
	IsDefault bool  `json:"isDefault"`
}

// DataScope 数据权限（总册 7.4）
type DataScope string

const (
	ScopeTenant  DataScope = "tenant"
	ScopeFactory DataScope = "factory"
	ScopeSelf    DataScope = "self"
)

type Role struct {
	BaseModel
	Code       string    `gorm:"uniqueIndex:uq_role;size:64" json:"code"`
	Name       string    `gorm:"size:64" json:"name"`
	DataScope  DataScope `gorm:"size:16;default:'factory'" json:"dataScope"`
	BuiltIn    bool      `json:"builtIn"`
	PermCodes  string    `gorm:"type:text" json:"-"` // 逗号分隔权限点
	Status     int16     `gorm:"default:1" json:"status"`
}

type UserRole struct {
	UserID int64 `gorm:"primaryKey" json:"userId"`
	RoleID int64 `gorm:"primaryKey" json:"roleId"`
}

type MenuType string

const (
	MenuDir   MenuType = "dir"
	MenuPage  MenuType = "page"
	MenuButton MenuType = "button"
)

type Menu struct {
	ID        int64    `gorm:"primaryKey" json:"id"`
	ParentID  int64    `gorm:"index;default:0" json:"parentId"`
	Name      string   `gorm:"size:64" json:"name"`
	Path      string   `gorm:"size:128" json:"path"`
	Icon      string   `gorm:"size:64" json:"icon"`
	Sort      int      `json:"sort"`
	Type      MenuType `gorm:"size:8" json:"type"`
	PermCode  string   `gorm:"size:64;index" json:"permCode"`
	Visible   bool     `gorm:"default:true" json:"visible"`
	CreatedAt time.Time `json:"createdAt"`
}

type AuditLog struct {
	ID         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID   int64     `gorm:"index" json:"tenantId"`
	FactoryID  *int64    `json:"factoryId"`
	UserID     int64     `json:"userId"`
	Username   string    `gorm:"size:64" json:"username"`
	Action     string    `gorm:"size:32;index" json:"action"`
	ObjectType string    `gorm:"size:64;index" json:"objectType"`
	ObjectID   string    `gorm:"size:64" json:"objectId"`
	ObjectDesc string    `gorm:"size:256" json:"objectDesc"`
	Before     string    `gorm:"type:jsonb;default:'{}'" json:"before"`
	After      string    `gorm:"type:jsonb;default:'{}'" json:"after"`
	IP         string    `gorm:"size:64" json:"ip"`
	CreatedAt  time.Time `gorm:"index" json:"createdAt"`
}

type SysParam struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	TenantID  int64     `gorm:"uniqueIndex:uq_param,priority:1" json:"tenantId"`
	FactoryID *int64    `gorm:"uniqueIndex:uq_param,priority:2" json:"factoryId"`
	Key       string    `gorm:"size:64;uniqueIndex:uq_param,priority:3" json:"key"`
	Value     string    `gorm:"type:jsonb;default:'{}'" json:"value"`
	Remark    string    `gorm:"size:256" json:"remark"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type DocNumberRule struct {
	ID            int64  `gorm:"primaryKey" json:"id"`
	TenantID      int64  `gorm:"uniqueIndex:uq_docrule,priority:1" json:"tenantId"`
	DocType       string `gorm:"size:16;uniqueIndex:uq_docrule,priority:2" json:"docType"`
	Prefix        string `gorm:"size:16" json:"prefix"`
	HasFactoryCode bool `json:"hasFactoryCode"`
	SeqLen        int   `gorm:"default:4" json:"seqLen"`
	CurrentSeq    int64 `json:"currentSeq"`
}

type IdempotencyKey struct {
	Key            string    `gorm:"primaryKey;size:64" json:"key"`
	TenantID       int64     `json:"tenantId"`
	UserID         int64     `json:"userId"`
	RequestHash    string    `gorm:"size:64" json:"requestHash"`
	ResponseStatus int       `json:"responseStatus"`
	ResponseBody   string    `json:"responseBody"`
	CreatedAt      time.Time `gorm:"index" json:"createdAt"`
}

// Attachment 单据附件（总册 7.3.1）：本地磁盘存储，EM_DATA_DIR 可改。
type Attachment struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	TenantID   int64     `gorm:"index" json:"tenantId"`
	ObjectType string    `gorm:"size:32;index:idx_attach,priority:1" json:"objectType"` // stock_order / work_order / sales_order …
	ObjectID   int64     `gorm:"index:idx_attach,priority:2" json:"objectId"`
	FileName   string    `gorm:"size:256" json:"fileName"`
	StoredPath string    `gorm:"size:512" json:"-"`
	Size       int64     `json:"size"`
	UploadedBy int64     `json:"uploadedBy"`
	CreatedAt  time.Time `json:"createdAt"`
}
