package model

import "time"

// ---------------- ERP 交易域（数据库设计 §3） ----------------

// 单据状态（各单据语义见 core-flows.md）
type DocStatus string

const (
	StDraft     DocStatus = "draft"
	StAudited   DocStatus = "audited"
	StPartial   DocStatus = "partial"
	StDone      DocStatus = "done"
	StClosed    DocStatus = "closed"
	StVoid      DocStatus = "void"
	// 采购专用
	StArriving  DocStatus = "arriving"
	StReceiving DocStatus = "receiving"
	// 请购专用
	StConverted DocStatus = "converted"
)

type SalesOrder struct {
	BaseModel
	DocNo        string    `gorm:"uniqueIndex:uq_so;size:32" json:"docNo"`
	CustomerID   int64     `gorm:"index" json:"customerId"`
	OrderDate    *time.Time `json:"orderDate"`
	DeliveryDate *time.Time `json:"deliveryDate"`
	TaxRate      float64   `gorm:"type:numeric(5,2);default:13" json:"taxRate"`
	Amount       float64   `gorm:"type:numeric(18,2);default:0" json:"amount"`
	Status       DocStatus `gorm:"size:16;default:'draft';index" json:"status"`
	AuditedBy    int64     `json:"auditedBy"`
	AuditedAt    *time.Time `json:"auditedAt"`
	Remark       string    `gorm:"size:256" json:"remark"`
	Lines        []SalesOrderLine `gorm:"constraint:OnDelete:CASCADE" json:"lines,omitempty"`
}

type SalesOrderLine struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	SalesOrderID int64     `gorm:"index" json:"salesOrderId"`
	LineNo       int       `json:"lineNo"`
	MaterialID   int64     `gorm:"index" json:"materialId"`
	Qty          float64   `gorm:"type:numeric(18,4)" json:"qty"`
	Price        float64   `gorm:"type:numeric(18,6)" json:"price"`
	Amount       float64   `gorm:"type:numeric(18,2)" json:"amount"`
	DeliveryDate *time.Time `json:"deliveryDate"`
	ShippedQty   float64   `gorm:"type:numeric(18,4);default:0" json:"shippedQty"`
	Remark       string    `gorm:"size:128" json:"remark"`
}

type PurchaseRequest struct {
	BaseModel
	DocNo      string    `gorm:"uniqueIndex:uq_pr;size:32" json:"docNo"`
	SourceType string    `gorm:"size:16;default:'manual'" json:"sourceType"` // manual|mrp
	Status     DocStatus `gorm:"size:16;default:'draft'" json:"status"`
	AuditedBy  int64     `json:"auditedBy"`
	AuditedAt  *time.Time `json:"auditedAt"`
	Remark     string    `gorm:"size:256" json:"remark"`
	Lines      []PurchaseRequestLine `gorm:"foreignKey:RequestID;constraint:OnDelete:CASCADE" json:"lines,omitempty"`
}

type PurchaseRequestLine struct {
	ID         int64   `gorm:"primaryKey" json:"id"`
	RequestID  int64   `gorm:"index" json:"requestId"`
	LineNo     int     `json:"lineNo"`
	MaterialID int64   `gorm:"index" json:"materialId"`
	Qty        float64 `gorm:"type:numeric(18,4)" json:"qty"`
	PurchasedQty float64 `gorm:"type:numeric(18,4);default:0" json:"purchasedQty"`
	Remark     string  `gorm:"size:128" json:"remark"`
}

type PurchaseOrder struct {
	BaseModel
	DocNo      string    `gorm:"uniqueIndex:uq_po;size:32" json:"docNo"`
	SupplierID int64     `gorm:"index" json:"supplierId"`
	OrderDate  *time.Time `json:"orderDate"`
	TaxRate    float64   `gorm:"type:numeric(5,2);default:13" json:"taxRate"`
	Amount     float64   `gorm:"type:numeric(18,2);default:0" json:"amount"`
	Status     DocStatus `gorm:"size:16;default:'draft';index" json:"status"`
	AuditedBy  int64     `json:"auditedBy"`
	AuditedAt  *time.Time `json:"auditedAt"`
	Remark     string    `gorm:"size:256" json:"remark"`
	Lines      []PurchaseOrderLine `gorm:"constraint:OnDelete:CASCADE" json:"lines,omitempty"`
}

type PurchaseOrderLine struct {
	ID            int64     `gorm:"primaryKey" json:"id"`
	PurchaseOrderID int64   `gorm:"index" json:"purchaseOrderId"`
	LineNo        int       `json:"lineNo"`
	MaterialID    int64     `gorm:"index" json:"materialId"`
	Qty           float64   `gorm:"type:numeric(18,4)" json:"qty"`
	Price         float64   `gorm:"type:numeric(18,6)" json:"price"`
	Amount        float64   `gorm:"type:numeric(18,2)" json:"amount"`
	DeliveryDate  *time.Time `json:"deliveryDate"`
	ReceivedQty   float64   `gorm:"type:numeric(18,4);default:0" json:"receivedQty"`
	Remark        string    `gorm:"size:128" json:"remark"`
}
