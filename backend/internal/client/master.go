// Package client 跨服务读通道（Phase 2.3 + master 分库批）。
//   - Local（默认/Lite）：直连共享库——单进程形态零开销、行为与旧版一致
//   - Remote（Micro）：HTTP 调 master-data /internal/master/*；引用数据（物料/工作中心/客商）
//     为小表，采用「全量快照 + 60s 缓存 + 单键索引」读法，聚合 SQL 在 core 侧 Go 合并
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"gorm.io/gorm"

	"evolvermon/backend/internal/infra"
	mapi "evolvermon/backend/internal/master/internalapi"
)

// MaterialLite 物料精简视图（覆盖 core 引用读所需全部字段）。
type MaterialLite = mapi.MaterialLite

// WCLite 工作中心精简视图。
type WCLite = mapi.WCLite

// PartnerLite 客商精简视图。
type PartnerLite = mapi.PartnerLite

// WhLite 仓库精简视图。
type WhLite = mapi.WhLite

// RoutingDTO 工艺路线（含工序）——工单创建快照用。
type RoutingDTO = mapi.RoutingDTO

// MasterData 主数据读通道（跨域边界接口）。
type MasterData interface {
	BomEdges(ctx context.Context, tenantID int64) ([]mapi.BomEdge, error)
	Materials(ctx context.Context, tenantID int64, ids []int64) ([]mapi.MaterialLite, error)
	MaterialMap(ctx context.Context, tenantID int64) (map[int64]mapi.MaterialLite, error) // 全量快照（60s 缓存）
	WorkCenters(ctx context.Context, tenantID int64) ([]mapi.WCLite, error)             // 全量（60s 缓存）
	RoutingOf(ctx context.Context, tenantID, materialID int64) (*mapi.RoutingDTO, error)
	BomOf(ctx context.Context, tenantID, bomID, materialID int64) (*mapi.BomDTO, error)
	Partner(ctx context.Context, tenantID int64, kind string, id int64) (*mapi.PartnerLite, error)
	Warehouse(ctx context.Context, id int64) (*mapi.WhLite, error)
	Location(ctx context.Context, id int64) (*mapi.LocLite, error)
	WarehousesByType(ctx context.Context, tenantID, factoryID int64, whType string) ([]mapi.WhLite, error)
	CountMaterials(ctx context.Context, tenantID int64) (int64, error)
}

// ---- Local（直连共享库；Lite / master-data 自身）----

type localMaster struct{ db *gorm.DB }

func (l *localMaster) BomEdges(_ context.Context, tenantID int64) ([]mapi.BomEdge, error) {
	var edges []mapi.BomEdge
	err := l.db.Raw(`SELECT b.material_id parent_id, l.material_id child_id, l.qty, l.scrap_rate
		FROM master.boms b JOIN master.bom_lines l ON l.bom_id=b.id
		WHERE b.tenant_id=? AND b.status='active' AND b.deleted_at IS NULL`, tenantID).Scan(&edges).Error
	return edges, err
}

func (l *localMaster) Materials(_ context.Context, tenantID int64, ids []int64) ([]mapi.MaterialLite, error) {
	return l.materials(tenantID, ids)
}

func (l *localMaster) materials(tenantID int64, ids []int64) ([]mapi.MaterialLite, error) {
	q := l.db.Table("master.materials").Select(mapi.MaterialCols).
		Where("tenant_id=? AND deleted_at IS NULL", tenantID)
	if len(ids) > 0 {
		q = q.Where("id IN ?", ids)
	}
	var rows []mapi.MaterialLite
	return rows, q.Scan(&rows).Error
}

func (l *localMaster) MaterialMap(_ context.Context, tenantID int64) (map[int64]mapi.MaterialLite, error) {
	rows, err := l.materials(tenantID, nil)
	return toMatMap(rows), err
}

func (l *localMaster) WorkCenters(_ context.Context, tenantID int64) ([]mapi.WCLite, error) {
	var rows []mapi.WCLite
	err := l.db.Table("master.work_centers").Select("id, tenant_id, factory_id, code, name, type, capacity_per_day, cost_rate, status").
		Where("tenant_id=? AND deleted_at IS NULL", tenantID).Scan(&rows).Error
	return rows, err
}

func (l *localMaster) RoutingOf(_ context.Context, tenantID, materialID int64) (*mapi.RoutingDTO, error) {
	var r mapi.RoutingDTO
	err := l.db.Raw(`SELECT r.id, r.tenant_id, r.material_id, r.version_no, r.status
		FROM master.routings r WHERE r.tenant_id=? AND r.material_id=? AND r.status='active' AND r.deleted_at IS NULL
		ORDER BY r.id DESC LIMIT 1`, tenantID, materialID).Scan(&r).Error
	if err != nil || r.ID == 0 {
		return nil, err
	}
	l.db.Raw(`SELECT o.id, o.routing_id, o.seq, o.name, o.work_center_id, o.prep_time_sec, o.run_time_per_unit_sec
		FROM master.routing_ops o WHERE o.routing_id=? ORDER BY o.seq`, r.ID).Scan(&r.Ops)
	return &r, nil
}

func (l *localMaster) BomOf(_ context.Context, tenantID, bomID, materialID int64) (*mapi.BomDTO, error) {
	var b mapi.BomDTO
	if bomID > 0 {
		l.db.Raw(`SELECT b.id, b.tenant_id, b.material_id, b.version_no, b.status
			FROM master.boms b WHERE b.id=? AND b.deleted_at IS NULL`, bomID).Scan(&b)
	} else {
		l.db.Raw(`SELECT b.id, b.tenant_id, b.material_id, b.version_no, b.status
			FROM master.boms b WHERE b.tenant_id=? AND b.material_id=? AND b.status='active' AND b.deleted_at IS NULL
			ORDER BY b.id DESC LIMIT 1`, tenantID, materialID).Scan(&b)
	}
	if b.ID > 0 {
		l.db.Raw(`SELECT l.id, l.bom_id, l.material_id, l.qty, l.scrap_rate
			FROM master.bom_lines l WHERE l.bom_id=? ORDER BY l.id`, b.ID).Scan(&b.Lines)
	}
	if b.ID == 0 {
		return nil, nil
	}
	return &b, nil
}

func (l *localMaster) Partner(_ context.Context, _ int64, kind string, id int64) (*mapi.PartnerLite, error) {
	var p mapi.PartnerLite
	err := l.db.Raw(`SELECT id, tenant_id, code, name, status FROM master.`+partnerTable(kind)+` WHERE id=? AND deleted_at IS NULL`, id).Scan(&p).Error
	if p.ID == 0 {
		return nil, err
	}
	return &p, nil
}

func (l *localMaster) Warehouse(_ context.Context, id int64) (*mapi.WhLite, error) {
	var w mapi.WhLite
	err := l.db.Raw(`SELECT id, tenant_id, factory_id, code, name, type, status FROM master.warehouses WHERE id=? AND deleted_at IS NULL`, id).Scan(&w).Error
	if w.ID == 0 {
		return nil, err
	}
	return &w, nil
}

func (l *localMaster) WarehousesByType(_ context.Context, tenantID, factoryID int64, whType string) ([]mapi.WhLite, error) {
	var rows []mapi.WhLite
	err := l.db.Raw(`SELECT id, tenant_id, factory_id, code, name, type, status FROM master.warehouses
		WHERE tenant_id=? AND factory_id=? AND type=? AND status=1 AND deleted_at IS NULL ORDER BY id LIMIT 1`, tenantID, factoryID, whType).Scan(&rows).Error
	return rows, err
}

func (l *localMaster) Location(_ context.Context, id int64) (*mapi.LocLite, error) {
	var w mapi.LocLite
	err := l.db.Raw(`SELECT id, warehouse_id, code, frozen FROM master.locations WHERE id=? AND deleted_at IS NULL`, id).Scan(&w).Error
	if w.ID == 0 {
		return nil, err
	}
	return &w, nil
}

func (l *localMaster) CountMaterials(_ context.Context, tenantID int64) (int64, error) {
	var n int64
	err := l.db.Raw(`SELECT COUNT(*) FROM master.materials WHERE tenant_id=? AND deleted_at IS NULL`, tenantID).Scan(&n).Error
	return n, err
}

func partnerTable(kind string) string {
	if kind == "supplier" {
		return "suppliers"
	}
	return "customers"
}

func toMatMap(rows []mapi.MaterialLite) map[int64]mapi.MaterialLite {
	m := make(map[int64]mapi.MaterialLite, len(rows))
	for _, r := range rows {
		m[r.ID] = r
	}
	return m
}

// ---- Remote（HTTP + 全量快照缓存）----

type remoteMaster struct {
	baseURL string
	token   string
	hc      *http.Client
	cache   infra.Cache
	ttl     time.Duration
	// 引用数据快照缓存（进程内；master 表为小表）
	mu     sync.RWMutex
	snapAt map[int64]time.Time
	mats   map[int64]map[int64]mapi.MaterialLite
	wcs    map[int64][]mapi.WCLite
}

func (r *remoteMaster) BomEdges(ctx context.Context, tenantID int64) ([]mapi.BomEdge, error) {
	key := fmt.Sprintf("master:bom-edges:%d", tenantID)
	if r.cache != nil {
		if raw, ok, _ := r.cache.Get(ctx, key); ok {
			var edges []mapi.BomEdge
			if json.Unmarshal(raw, &edges) == nil {
				return edges, nil
			}
		}
	}
	var out struct {
		Data struct {
			List []mapi.BomEdge `json:"list"`
		} `json:"data"`
	}
	if err := r.get(ctx, fmt.Sprintf("/internal/master/bom-edges?tenantId=%d", tenantID), &out); err != nil {
		return nil, err
	}
	if r.cache != nil {
		if b, err := json.Marshal(out.Data.List); err == nil {
			_ = r.cache.Set(ctx, key, b, r.ttl)
		}
	}
	return out.Data.List, nil
}

func (r *remoteMaster) Materials(ctx context.Context, tenantID int64, ids []int64) ([]mapi.MaterialLite, error) {
	idQuery := ""
	for i, id := range ids {
		if i > 0 {
			idQuery += ","
		}
		idQuery += fmt.Sprint(id)
	}
	var out struct {
		Data struct {
			List []mapi.MaterialLite `json:"list"`
		} `json:"data"`
	}
	if err := r.get(ctx, fmt.Sprintf("/internal/master/materials?tenantId=%d&ids=%s", tenantID, idQuery), &out); err != nil {
		return nil, err
	}
	return out.Data.List, nil
}

func (r *remoteMaster) MaterialMap(ctx context.Context, tenantID int64) (map[int64]mapi.MaterialLite, error) {
	r.mu.RLock()
	if m, ok := r.mats[tenantID]; ok && time.Since(r.snapAt[tenantID]) < snapTTL {
		r.mu.RUnlock()
		return m, nil
	}
	r.mu.RUnlock()
	var out struct {
		Data struct {
			List []mapi.MaterialLite `json:"list"`
		} `json:"data"`
	}
	if err := r.get(ctx, fmt.Sprintf("/internal/master/materials?tenantId=%d", tenantID), &out); err != nil {
		return nil, err
	}
	m := toMatMap(out.Data.List)
	r.mu.Lock()
	if r.mats == nil {
		r.mats = map[int64]map[int64]mapi.MaterialLite{}
		r.snapAt = map[int64]time.Time{}
	}
	r.mats[tenantID], r.snapAt[tenantID] = m, time.Now()
	r.mu.Unlock()
	return m, nil
}

func (r *remoteMaster) WorkCenters(ctx context.Context, tenantID int64) ([]mapi.WCLite, error) {
	r.mu.RLock()
	if wcs, ok := r.wcs[tenantID]; ok && time.Since(r.snapAt[tenantID]) < snapTTL {
		r.mu.RUnlock()
		return wcs, nil
	}
	r.mu.RUnlock()
	var out struct {
		Data struct {
			List []mapi.WCLite `json:"list"`
		} `json:"data"`
	}
	if err := r.get(ctx, fmt.Sprintf("/internal/master/work-centers?tenantId=%d", tenantID), &out); err != nil {
		return nil, err
	}
	r.mu.Lock()
	if r.wcs == nil {
		r.wcs = map[int64][]mapi.WCLite{}
		r.snapAt = map[int64]time.Time{}
	}
	r.wcs[tenantID] = out.Data.List
	r.snapAt[tenantID] = time.Now()
	r.mu.Unlock()
	return out.Data.List, nil
}

func (r *remoteMaster) RoutingOf(ctx context.Context, tenantID, materialID int64) (*mapi.RoutingDTO, error) {
	var out struct {
	Data struct {
			Value mapi.RoutingDTO `json:"value"`
		} `json:"data"`
	}
	if err := r.get(ctx, fmt.Sprintf("/internal/master/routing?tenantId=%d&materialId=%d", tenantID, materialID), &out); err != nil {
		return nil, err
	}
	if out.Data.Value.ID == 0 {
		return nil, nil
	}
	return &out.Data.Value, nil
}

func (r *remoteMaster) BomOf(ctx context.Context, tenantID, bomID, materialID int64) (*mapi.BomDTO, error) {
	var out struct {
		Data struct {
			Value mapi.BomDTO `json:"value"`
		} `json:"data"`
	}
	if err := r.get(ctx, fmt.Sprintf("/internal/master/bom?tenantId=%d&id=%d&materialId=%d", tenantID, bomID, materialID), &out); err != nil {
		return nil, err
	}
	if out.Data.Value.ID == 0 {
		return nil, nil
	}
	return &out.Data.Value, nil
}

func (r *remoteMaster) Partner(ctx context.Context, tenantID int64, kind string, id int64) (*mapi.PartnerLite, error) {
	var out struct {
		Data struct {
			Value mapi.PartnerLite `json:"value"`
		} `json:"data"`
	}
	if err := r.get(ctx, fmt.Sprintf("/internal/master/partner?tenantId=%d&kind=%s&id=%d", tenantID, kind, id), &out); err != nil {
		return nil, err
	}
	if out.Data.Value.ID == 0 {
		return nil, nil
	}
	return &out.Data.Value, nil
}

func (r *remoteMaster) Warehouse(ctx context.Context, id int64) (*mapi.WhLite, error) {
	var out struct {
		Data struct {
			Value mapi.WhLite `json:"value"`
		} `json:"data"`
	}
	if err := r.get(ctx, fmt.Sprintf("/internal/master/warehouse?id=%d", id), &out); err != nil {
		return nil, err
	}
	if out.Data.Value.ID == 0 {
		return nil, nil
	}
	return &out.Data.Value, nil
}

func (r *remoteMaster) CountMaterials(ctx context.Context, tenantID int64) (int64, error) {
	m, err := r.MaterialMap(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	return int64(len(m)), nil
}

func (r *remoteMaster) WarehousesByType(ctx context.Context, tenantID, factoryID int64, whType string) ([]mapi.WhLite, error) {
	var out struct {
		Data struct {
			List []mapi.WhLite `json:"list"`
		} `json:"data"`
	}
	if err := r.get(ctx, fmt.Sprintf("/internal/master/warehouses-by-type?tenantId=%d&factoryId=%d&type=%s", tenantID, factoryID, whType), &out); err != nil {
		return nil, err
	}
	return out.Data.List, nil
}

func (r *remoteMaster) Location(ctx context.Context, id int64) (*mapi.LocLite, error) {
	var out struct {
		Data struct {
			Value mapi.LocLite `json:"value"`
		} `json:"data"`
	}
	if err := r.get(ctx, fmt.Sprintf("/internal/master/location?id=%d", id), &out); err != nil {
		return nil, err
	}
	if out.Data.Value.ID == 0 {
		return nil, nil
	}
	return &out.Data.Value, nil
}

func (r *remoteMaster) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Internal-Token", r.token)
	resp, err := r.hc.Do(req)
	if err != nil {
		return fmt.Errorf("master-data %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("master-data %s: http %d", path, resp.StatusCode)
	}
	return json.Unmarshal(body, out)
}

// snapTTL 引用数据快照缓存时长（小表全量，60s）。
const snapTTL = 60 * time.Second

// ---- 装配 ----

var sharedCache infra.Cache

// UseCache 注入进程级缓存端口（各 main 启动时调用；nil=Remote 不缓存）。
func UseCache(c infra.Cache) { sharedCache = c }

var (
	defaultOnce sync.Once
	defaultMD   MasterData
)

// SetDefaultMaster 显式设置默认通道（测试用）。
func SetDefaultMaster(md MasterData) { defaultMD = md }

// NewMaster 按环境装配：EM_MASTER_URL 配置 → Remote（+缓存），否则 Local。
func NewMaster(db *gorm.DB) MasterData {
	if u := os.Getenv("EM_MASTER_URL"); u != "" {
		return &remoteMaster{
			baseURL: u, token: os.Getenv("EM_INTERNAL_TOKEN"),
			hc: &http.Client{Timeout: 5 * time.Second},
			cache: sharedCache, ttl: 60 * time.Second,
		}
	}
	return &localMaster{db: db}
}

// M 默认通道（api 层便捷入口；首次调用按环境装配，须先 InitDefaultMaster）。
func M() MasterData {
	defaultOnce.Do(func() {
		if defaultMD == nil {
			defaultMD = NewMaster(nil)
		}
	})
	return defaultMD
}

// InitDefaultMaster 进程启动时装配默认通道（main 调用；Lite=Local 直连）。
func InitDefaultMaster(db *gorm.DB) {
	defaultOnce.Do(func() { defaultMD = NewMaster(db) })
}

// ---- 顶层便捷引用读（找不到返回 ErrNotFound 语义）----

// ErrNotFound 引用不存在。
var ErrNotFound = fmt.Errorf("master ref not found")

// MatRef 单物料引用读。
func MatRef(ctx context.Context, tenantID, id int64) (MaterialLite, error) {
	rows, err := M().Materials(ctx, tenantID, []int64{id})
	if err != nil {
		return MaterialLite{}, err
	}
	if len(rows) == 0 {
		return MaterialLite{}, ErrNotFound
	}
	return rows[0], nil
}

// WhRef 单仓库引用读。
func WhRef(ctx context.Context, id int64) (WhLite, error) {
	w, err := M().Warehouse(ctx, id)
	if err != nil {
		return WhLite{}, err
	}
	if w == nil {
		return WhLite{}, ErrNotFound
	}
	return *w, nil
}

// PartnerRef 客商引用读（kind=customer|supplier）。
func PartnerRef(ctx context.Context, tenantID int64, kind string, id int64) (PartnerLite, error) {
	p, err := M().Partner(ctx, tenantID, kind, id)
	if err != nil {
		return PartnerLite{}, err
	}
	if p == nil {
		return PartnerLite{}, ErrNotFound
	}
	return *p, nil
}

// WhByType 按工厂+类型取首个可用仓（未配置返回 ErrNotFound）。
func WhByType(ctx context.Context, tenantID, factoryID int64, whType string) (WhLite, error) {
	rows, err := M().WarehousesByType(ctx, tenantID, factoryID, whType)
	if err != nil {
		return WhLite{}, err
	}
	if len(rows) == 0 {
		return WhLite{}, ErrNotFound
	}
	return rows[0], nil
}

// LocLite 货位精简视图。
type LocLite = mapi.LocLite

// LocationRef 单货位（冻结校验高频路径；Remote 走端点）。
func LocationRef(ctx context.Context, id int64) (LocLite, error) {
	l, err := M().Location(ctx, id)
	if err != nil {
		return LocLite{}, err
	}
	if l == nil {
		return LocLite{}, ErrNotFound
	}
	return *l, nil
}
