package model

import "time"

// ---------------- ERP 主数据域（数据库设计 §2） ----------------

type MaterialCategory string

const (
	MatRaw     MaterialCategory = "raw"
	MatSemi    MaterialCategory = "semi"
	MatFinish  MaterialCategory = "finished"
	MatAux     MaterialCategory = "aux"
	MatPackage MaterialCategory = "package"
)

type SourceType string

const (
	SrcMake   SourceType = "make"
	SrcBuy    SourceType = "buy"
	SrcOutsrc SourceType = "outsource"
)

type Material struct {
	BaseModel
	Code              string           `gorm:"uniqueIndex:uq_mat;size:64" json:"code"`
	Name              string           `gorm:"size:128;index" json:"name"`
	Spec              string           `gorm:"size:128" json:"spec"`
	Category          MaterialCategory `gorm:"size:16;index" json:"category"`
	Unit              string           `gorm:"size:16" json:"unit"`
	SourceType        SourceType       `gorm:"size:16;default:'buy'" json:"sourceType"`
	BatchManaged      bool             `json:"batchManaged"`
	SerialManaged     bool             `gorm:"default:false" json:"serialManaged"`
	SafetyStock       float64          `gorm:"type:numeric(18,4);default:0" json:"safetyStock"`
	DefaultWarehouseID *int64          `json:"defaultWarehouseId"`
	Barcode           string           `gorm:"size:64" json:"barcode"`
	Price             float64          `gorm:"type:numeric(18,6);default:0" json:"price"`
	LeadTimeDays      int              `gorm:"default:0" json:"leadTimeDays"`
	Status            int16            `gorm:"default:1;index" json:"status"`
	Extra             string           `gorm:"type:jsonb;default:'{}'" json:"extra"`
}

type BomStatus string

const (
	BomDraft  BomStatus = "draft"
	BomActive BomStatus = "active"
	BomOff    BomStatus = "off"
)

type Bom struct {
	BaseModel
	MaterialID    int64     `gorm:"index" json:"materialId"`
	VersionNo     string    `gorm:"size:16" json:"versionNo"`
	Status        BomStatus `gorm:"size:16;default:'draft';index" json:"status"`
	EffectiveFrom *time.Time `json:"effectiveFrom"`
	Remark        string    `gorm:"size:256" json:"remark"`
	Lines         []BomLine `gorm:"constraint:OnDelete:CASCADE" json:"lines,omitempty"`
}

type BomLine struct {
	ID         int64   `gorm:"primaryKey" json:"id"`
	BomID      int64   `gorm:"index" json:"bomId"`
	LineNo     int     `json:"lineNo"`
	MaterialID int64   `gorm:"index" json:"materialId"`
	Qty        float64 `gorm:"type:numeric(18,6)" json:"qty"`
	ScrapRate float64 `gorm:"type:numeric(5,2);default:0" json:"scrapRate"`
	Remark     string  `gorm:"size:128" json:"remark"`
}

type Routing struct {
	BaseModel
	MaterialID int64         `gorm:"index" json:"materialId"`
	BomID      *int64        `json:"bomId"`
	VersionNo  string        `gorm:"size:16" json:"versionNo"`
	Status     BomStatus     `gorm:"size:16;default:'active'" json:"status"`
	Remark     string        `gorm:"size:256" json:"remark"`
	Ops        []RoutingOp   `gorm:"constraint:OnDelete:CASCADE" json:"ops,omitempty"`
}

type RoutingOp struct {
	ID                int64   `gorm:"primaryKey" json:"id"`
	RoutingID         int64   `gorm:"index" json:"routingId"`
	Seq               int     `json:"seq"`
	Name              string  `gorm:"size:64" json:"name"`
	WorkCenterID      int64   `gorm:"index" json:"workCenterId"`
	PrepTimeSec       int     `json:"prepTimeSec"`
	RunTimePerUnitSec float64 `gorm:"type:numeric(10,3);default:0" json:"runTimePerUnitSec"`
	IsOutsource       bool    `json:"isOutsource"`
	PieceRate         float64 `gorm:"type:numeric(12,2);default:0" json:"pieceRate"`
}

type WorkCenterType string

const (
	WcLabor WorkCenterType = "labor"
	WcMachine WorkCenterType = "machine"
	WcLine  WorkCenterType = "line"
)

type WorkCenter struct {
	BaseModel
	Code           string          `gorm:"uniqueIndex:uq_wc;size:32" json:"code"`
	Name           string          `gorm:"size:128" json:"name"`
	Type           WorkCenterType  `gorm:"size:16;default:'machine'" json:"type"`
	CapacityPerDay float64         `gorm:"type:numeric(12,2);default:0" json:"capacityPerDay"`
	CostRate       float64         `gorm:"type:numeric(12,2);default:0" json:"costRate"`
	Status         int16           `gorm:"default:1" json:"status"`
}

type Customer struct {
	BaseModel
	Code        string  `gorm:"uniqueIndex:uq_cust;size:32" json:"code"`
	Name        string  `gorm:"size:128;index" json:"name"`
	Contact     string  `gorm:"size:64" json:"contact"`
	Phone       string  `gorm:"size:32" json:"phone"`
	TaxNo       string  `gorm:"size:64" json:"taxNo"`
	CreditLimit float64 `gorm:"type:numeric(18,2);default:0" json:"creditLimit"`
	Status      int16   `gorm:"default:1" json:"status"`
	Extra       string  `gorm:"type:jsonb;default:'{}'" json:"extra"`
}

type Supplier struct {
	BaseModel
	Code         string `gorm:"uniqueIndex:uq_supp;size:32" json:"code"`
	Name         string `gorm:"size:128;index" json:"name"`
	Contact      string `gorm:"size:64" json:"contact"`
	Phone        string `gorm:"size:32" json:"phone"`
	LeadTimeDays int    `gorm:"default:7" json:"leadTimeDays"`
	PaymentTerm  string `gorm:"size:128" json:"paymentTerm"`
	Status       int16  `gorm:"default:1" json:"status"`
}
