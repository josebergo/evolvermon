// Package client 跨服务读通道（Phase 2.3）：core/scheduler 读主数据统一经 MasterData 接口。
//   - Local（默认/Lite）：直连共享库——单进程形态零开销、行为与旧版一致
//   - Remote（Micro）：HTTP 调 master-data /internal/master/*，经 infra.Cache 端口缓存
//     （TTL 60s；事件失效随远端 Broker 落地，见执行方案 2.3 备注）
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/infra"
	mapi "github.com/evolvermon/evolvermon/internal/master/internalapi"
)

// MasterData 主数据读通道（跨域边界接口；新增读法先加此处再各实现）。
type MasterData interface {
	BomEdges(ctx context.Context, tenantID int64) ([]mapi.BomEdge, error)
	Materials(ctx context.Context, tenantID int64, ids []int64) ([]mapi.MaterialLite, error)
}

// ---- Local ----

type localMaster struct{ db *gorm.DB }

func (l *localMaster) BomEdges(_ context.Context, tenantID int64) ([]mapi.BomEdge, error) {
	var edges []mapi.BomEdge
	err := l.db.Raw(`SELECT b.material_id parent_id, l.material_id child_id, l.qty, l.scrap_rate
		FROM master.boms b JOIN master.bom_lines l ON l.bom_id=b.id
		WHERE b.tenant_id=? AND b.status='active' AND b.deleted_at IS NULL`, tenantID).Scan(&edges).Error
	return edges, err
}

func (l *localMaster) Materials(_ context.Context, tenantID int64, ids []int64) ([]mapi.MaterialLite, error) {
	q := l.db.Table("master.materials").
		Select("id, code, name, category, price, safety_stock, serial_managed").
		Where("tenant_id=? AND deleted_at IS NULL", tenantID)
	if len(ids) > 0 {
		q = q.Where("id IN ?", ids)
	}
	var rows []mapi.MaterialLite
	return rows, q.Scan(&rows).Error
}

// ---- Remote（HTTP + 缓存）----

type remoteMaster struct {
	baseURL string
	token   string
	hc      *http.Client
	cache   infra.Cache
	ttl     time.Duration
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
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("master-data %s: http %d", path, resp.StatusCode)
	}
	return json.Unmarshal(body, out)
}

// ---- 装配 ----

var sharedCache infra.Cache

// UseCache 注入进程级缓存端口（各 main 启动时调用；nil=Remote 不缓存）。
func UseCache(c infra.Cache) { sharedCache = c }

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
