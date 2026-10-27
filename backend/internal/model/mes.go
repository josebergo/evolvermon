package model

import "time"

// ---------------- MES 域（数据库设计 §4） ----------------

type WoStatus string

const (
	WoDraft   WoStatus = "draft"
	WoReleased WoStatus = "released"
	WoRunning WoStatus = "running"
	WoPaused  WoStatus = "paused"
	WoFinished WoStatus = "finished"
	WoStored  WoStatus = "stored"
	WoClosed  WoStatus = "closed"
	WoVoid    WoStatus = "void"
)

type WorkOrder struct {
	BaseModel
	DocNo            string    `gorm:"uniqueIndex:uq_wo;size:32" json:"docNo"`
	MaterialID       int64     `gorm:"index" json:"materialId"`
	Qty              float64   `gorm:"type:numeric(18,4)" json:"qty"`
	BomID            *int64    `json:"bomId"`
	BomSnapshot      string    `gorm:"type:jsonb;default:'{}'" json:"bomSnapshot"`
	RoutingID        *int64    `json:"routingId"`
	RoutingSnapshot  string    `gorm:"type:jsonb;default:'{}'" json:"routingSnapshot"`
	SourceType       string    `gorm:"size:16;default:'manual'" json:"sourceType"` // manual|mrp
	SourceDocNo      string    `gorm:"size:64" json:"sourceDocNo"`
	Priority         int       `gorm:"default:5" json:"priority"`
	PlanStartAt      *time.Time `json:"planStartAt"`
	PlanEndAt        *time.Time `json:"planEndAt"`
	ActualStartAt    *time.Time `json:"actualStartAt"`
	ActualEndAt      *time.Time `json:"actualEndAt"`
	Status           WoStatus  `gorm:"size:16;default:'draft';index" json:"status"`
	ReportedQty      float64   `gorm:"type:numeric(18,4);default:0" json:"reportedQty"`
	GoodQty          float64   `gorm:"type:numeric(18,4);default:0" json:"goodQty"`
	DefectQty        float64   `gorm:"type:numeric(18,4);default:0" json:"defectQty"`
	PickedQty        float64   `gorm:"type:numeric(18,4);default:0" json:"pickedQty"`
	MaterialCost     float64   `gorm:"type:numeric(18,4);default:0" json:"materialCost"` // M4：实际成本归集
	LaborCost        float64   `gorm:"type:numeric(18,4);default:0" json:"laborCost"`
	OverheadCost     float64   `gorm:"type:numeric(18,4);default:0" json:"overheadCost"`
	Remark           string    `gorm:"size:256" json:"remark"`
	Ops              []WoOperation `gorm:"constraint:OnDelete:CASCADE" json:"ops,omitempty"`
}

type OpStatus string

const (
	OpWaiting  OpStatus = "waiting"
	OpAssigned OpStatus = "assigned"
	OpRunning  OpStatus = "running"
	OpDone     OpStatus = "done"
	OpSkipped  OpStatus = "skipped"
)

type WoOperation struct {
	ID              int64      `gorm:"primaryKey" json:"id"`
	WorkOrderID     int64      `gorm:"index" json:"workOrderId"`
	Seq             int        `json:"seq"`
	Name            string     `gorm:"size:64" json:"name"`
	WorkCenterID    int64      `json:"workCenterId"`
	PlanHours       float64    `gorm:"type:numeric(10,2)" json:"planHours"`
	PlanStartAt     *time.Time `json:"planStartAt"` // M3：有限产能排产写入
	PlanEndAt       *time.Time `json:"planEndAt"`
	Status          OpStatus   `gorm:"size:16;default:'waiting';index" json:"status"`
	AssigneeUserID  *int64     `gorm:"index" json:"assigneeUserId"`
	StartedAt       *time.Time `json:"startedAt"`
	EndedAt         *time.Time `json:"endedAt"`
	QtyOk           float64    `gorm:"type:numeric(18,4);default:0" json:"qtyOk"`
	QtyDefect       float64    `gorm:"type:numeric(18,4);default:0" json:"qtyDefect"`
}

type ProductionReport struct {
	ID            int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID      int64     `gorm:"index" json:"tenantId"`
	FactoryID     *int64    `gorm:"index" json:"factoryId"`
	WorkOrderID   int64     `gorm:"index:idx_wo_reported" json:"workOrderId"`
	WoOperationID int64     `gorm:"index" json:"woOperationId"`
	UserID        int64     `gorm:"index" json:"userId"`
	Username      string    `gorm:"size:64" json:"username"`
	QtyOk         float64   `gorm:"type:numeric(18,4)" json:"qtyOk"`
	QtyDefect     float64   `gorm:"type:numeric(18,4)" json:"qtyDefect"`
	DefectCodes   string    `gorm:"size:256" json:"defectCodes"`
	Hours         float64   `gorm:"type:numeric(8,2)" json:"hours"`
	ClientToken   string    `gorm:"uniqueIndex;size:64" json:"clientToken"`
	ReportedAt    time.Time `gorm:"index:idx_wo_reported" json:"reportedAt"`
	Remark        string    `gorm:"size:256" json:"remark"`
}

type DefectCode struct {
	BaseModel
	Code     string `gorm:"uniqueIndex:uq_dc;size:32" json:"code"`
	Name     string `gorm:"size:64" json:"name"`
	Category string `gorm:"size:32" json:"category"`
	Sort     int    `json:"sort"`
	Status   int16  `gorm:"default:1" json:"status"`
}

type QcCheckType string

const (
	QcIQC QcCheckType = "iqc"
	QcIPQC QcCheckType = "ipqc"
	QcFQC QcCheckType = "fqc"
)

type QcScheme struct {
	BaseModel
	MaterialID  int64       `gorm:"index" json:"materialId"`
	CheckType   QcCheckType `gorm:"size:8" json:"checkType"`
	SampleRatio float64     `gorm:"type:numeric(5,4);default:0.1" json:"sampleRatio"`
	Items       string      `gorm:"type:jsonb;default:'{}'" json:"items"` // [{name,std,method}]
	Remark      string      `gorm:"size:256" json:"remark"`
}

type QcResult string

const (
	QcPending  QcResult = "pending"
	QcPass     QcResult = "pass"
	QcFail     QcResult = "fail"
	QcConcession QcResult = "concession"
)

type QcRecord struct {
	BaseModel
	DocNo          string    `gorm:"uniqueIndex:uq_qc;size:32" json:"docNo"`
	SourceType     string    `gorm:"size:16" json:"sourceType"` // arrive|operation|finish|manual
	SourceDocType  string    `gorm:"size:32" json:"sourceDocType"`
	SourceDocID    int64     `gorm:"index" json:"sourceDocId"`
	SourceDocNo    string    `gorm:"size:32" json:"sourceDocNo"`
	WorkOrderID    *int64    `gorm:"index" json:"workOrderId"`
	MaterialID     int64     `gorm:"index" json:"materialId"`
	BatchNo        string    `gorm:"size:64;index" json:"batchNo"`
	Qty            float64   `gorm:"type:numeric(18,4)" json:"qty"`
	InspectorID    int64     `json:"inspectorId"`
	Result         QcResult  `gorm:"size:16;default:'pending'" json:"result"`
	ItemsResult    string    `gorm:"type:jsonb;default:'{}'" json:"itemsResult"`
	Disposal       string    `gorm:"size:16" json:"disposal"` // rework|scrap|concession
	AuditedAt      *time.Time `json:"auditedAt"`
	Remark         string    `gorm:"size:256" json:"remark"`
}
