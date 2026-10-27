package model

import "time"

// ---------------- WMS 域（数据库设计 §5） ----------------

type Stock struct {
	ID           int64   `gorm:"primaryKey" json:"id"`
	TenantID     int64   `gorm:"index:idx_stock_dim,priority:1" json:"tenantId"`
	FactoryID    int64   `gorm:"index:idx_stock_dim,priority:2" json:"factoryId"`
	WarehouseID  int64   `gorm:"index:idx_stock_dim,priority:3" json:"warehouseId"`
	LocationID   *int64  `gorm:"index:idx_stock_dim,priority:4" json:"locationId"`
	MaterialID   int64   `gorm:"index:idx_stock_dim,priority:5" json:"materialId"`
	BatchNo      string  `gorm:"size:64;index:idx_stock_dim,priority:6" json:"batchNo"`
	QtyOnHand    float64 `gorm:"type:numeric(18,4);default:0" json:"qtyOnHand"`
	QtyReserved  float64 `gorm:"type:numeric(18,4);default:0" json:"qtyReserved"`
	QtyFrozen    float64 `gorm:"type:numeric(18,4);default:0" json:"qtyFrozen"`
	QtyDefective float64 `gorm:"type:numeric(18,4);default:0" json:"qtyDefective"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type MoveDirection string

const (
	MoveIn  MoveDirection = "IN"
	MoveOut MoveDirection = "OUT"
)

type StockMovement struct {
	ID             int64         `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID       int64         `gorm:"index" json:"tenantId"`
	FactoryID      int64         `gorm:"index" json:"factoryId"`
	WarehouseID    int64         `gorm:"index" json:"warehouseId"`
	LocationID     *int64        `json:"locationId"`
	MaterialID     int64         `gorm:"index:idx_move_mat" json:"materialId"`
	BatchNo        string        `gorm:"size:64" json:"batchNo"`
	Direction      MoveDirection `gorm:"size:4" json:"direction"`
	Qty            float64       `gorm:"type:numeric(18,4)" json:"qty"`
	BizType        string        `gorm:"size:32;index" json:"bizType"`
	SourceDocType  string        `gorm:"size:32;index" json:"sourceDocType"`
	SourceDocID    int64         `gorm:"index" json:"sourceDocId"`
	SourceDocNo    string        `gorm:"size:32" json:"sourceDocNo"`
	BeforeQty      float64       `gorm:"type:numeric(18,4)" json:"beforeQty"`
	AfterQty       float64       `gorm:"type:numeric(18,4)" json:"afterQty"`
	OpUserID       int64         `json:"opUserId"`
	CreatedAt      time.Time     `gorm:"index:idx_move_mat,priority:2" json:"createdAt"`
}

type StockDocType string

const (
	DocRCV StockDocType = "rcv" // 入库
	DocSHP StockDocType = "shp" // 出库
	DocMV  StockDocType = "mv"  // 移库
)

type StockStatus string

const (
	SkDraft   StockStatus = "draft"
	SkDoing   StockStatus = "doing"
	SkDone    StockStatus = "done"
	SkVoid    StockStatus = "void"
)

type StockOrder struct {
	BaseModel
	DocType        StockDocType `gorm:"size:8;index" json:"docType"`
	DocNo          string       `gorm:"uniqueIndex:uq_stockdoc;size:32" json:"docNo"`
	WarehouseID    int64        `gorm:"index" json:"warehouseId"`
	SourceType     string       `gorm:"size:16;index" json:"sourceType"` // purchase|production|sales|picking|scrap|gain|loss|move|manual
	SourceDocType  string       `gorm:"size:32" json:"sourceDocType"`
	SourceDocID    int64        `gorm:"index" json:"sourceDocId"`
	SourceDocNo    string       `gorm:"size:32" json:"sourceDocNo"`
	ToWarehouseID  *int64       `json:"toWarehouseId"`
	Status         StockStatus  `gorm:"size:16;default:'draft';index" json:"status"`
	PostedBy       int64        `json:"postedBy"`
	PostedAt       *time.Time   `json:"postedAt"`
	Remark         string       `gorm:"size:256" json:"remark"`
	Lines          []StockOrderLine `gorm:"constraint:OnDelete:CASCADE" json:"lines,omitempty"`
}

type StockOrderLine struct {
	ID             int64   `gorm:"primaryKey" json:"id"`
	StockOrderID   int64   `gorm:"index" json:"stockOrderId"`
	LineNo         int     `json:"lineNo"`
	MaterialID     int64   `gorm:"index" json:"materialId"`
	BatchNo        string  `gorm:"size:64" json:"batchNo"`
	Qty            float64 `gorm:"type:numeric(18,4)" json:"qty"`
	DoneQty        float64 `gorm:"type:numeric(18,4);default:0" json:"doneQty"`
	FromLocationID *int64  `json:"fromLocationId"`
	ToLocationID   *int64  `json:"toLocationId"`
	Price          float64 `gorm:"type:numeric(18,6);default:0" json:"price"`
	Remark         string  `gorm:"size:128" json:"remark"`
}

type Stocktaking struct {
	BaseModel
	DocNo      string  `gorm:"uniqueIndex:uq_stk;size:32" json:"docNo"`
	WarehouseID int64 `gorm:"index" json:"warehouseId"`
	Scope      string `gorm:"size:8;default:'full'" json:"scope"`  // full|sample
	Method     string `gorm:"size:8;default:'blind'" json:"method"` // blind|open
	Status     string `gorm:"size:16;default:'draft';index" json:"status"` // draft|counting|confirmed|adjusted
	SnapshotAt *time.Time `json:"snapshotAt"`
	Remark     string  `gorm:"size:256" json:"remark"`
	Lines      []StocktakingLine `gorm:"constraint:OnDelete:CASCADE" json:"lines,omitempty"`
}

type StocktakingLine struct {
	ID           int64    `gorm:"primaryKey" json:"id"`
	StocktakingID int64   `gorm:"index" json:"stocktakingId"`
	LocationID   *int64   `json:"locationId"`
	MaterialID   int64    `gorm:"index" json:"materialId"`
	BatchNo      string   `gorm:"size:64" json:"batchNo"`
	BookQty      float64  `gorm:"type:numeric(18,4)" json:"bookQty"`
	CountedQty   *float64 `gorm:"type:numeric(18,4)" json:"countedQty"`
	Adjusted     bool     `gorm:"default:false" json:"adjusted"`
}

type ReservationStatus string

const (
	RvHeld     ReservationStatus = "held"
	RvReleased ReservationStatus = "released"
	RvConsumed ReservationStatus = "consumed"
)

type StockReservation struct {
	ID            int64             `gorm:"primaryKey" json:"id"`
	TenantID      int64             `gorm:"index" json:"tenantId"`
	FactoryID     int64             `gorm:"index" json:"factoryId"`
	MaterialID    int64             `gorm:"index" json:"materialId"`
	WarehouseID   *int64            `json:"warehouseId"`
	Qty           float64           `gorm:"type:numeric(18,4)" json:"qty"`
	BizType       string            `gorm:"size:16" json:"bizType"` // sales|workorder
	SourceDocType string            `gorm:"size:32" json:"sourceDocType"`
	SourceDocID   int64             `gorm:"index" json:"sourceDocId"`
	SourceLineID  int64             `json:"sourceLineId"`
	Status        ReservationStatus `gorm:"size:16;default:'held'" json:"status"`
	CreatedAt     time.Time         `json:"createdAt"`
	ReleasedAt    *time.Time        `json:"releasedAt"`
}
