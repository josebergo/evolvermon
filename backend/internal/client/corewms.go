// Package client — core-wms 读通道（Phase 3 批 1）：erp/mes 读 wms 聚合经此，
// 三域拆库后为唯一路径；Lite/core 单进程 = Local 直查。EM_WMS_URL 配置远端（core-wms 进程）。
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"gorm.io/gorm"
)

// StockSum 物料汇总量。
type StockSum struct {
	MaterialID int64   `json:"materialId"`
	Qty        float64 `json:"qty"`
}

// CoreWMS wms 域读通道。
type CoreWMS interface {
	StockSums(ctx context.Context, tenantID, factoryID int64) ([]StockSum, error)
	ReservedSums(ctx context.Context, tenantID, factoryID int64) ([]StockSum, error)
	StockSingle(ctx context.Context, tenantID, factoryID, materialID int64) (float64, error)
	StockOrderTodos(ctx context.Context, tenantID, factoryID int64) (rcv, shp int64, err error)
}

type localWMS struct{ db *gorm.DB }

func (l *localWMS) StockSums(_ context.Context, tenantID, factoryID int64) ([]StockSum, error) {
	var rows []StockSum
	err := l.db.Raw(`SELECT material_id, SUM(qty_on_hand) qty FROM wms.stocks WHERE tenant_id=? AND factory_id=? GROUP BY material_id`, tenantID, factoryID).Scan(&rows).Error
	return rows, err
}

func (l *localWMS) ReservedSums(_ context.Context, tenantID, factoryID int64) ([]StockSum, error) {
	var rows []StockSum
	err := l.db.Raw(`SELECT material_id, SUM(qty) qty FROM wms.stock_reservations WHERE tenant_id=? AND factory_id=? AND status='held' GROUP BY material_id`, tenantID, factoryID).Scan(&rows).Error
	return rows, err
}

func (l *localWMS) StockSingle(_ context.Context, tenantID, factoryID, materialID int64) (float64, error) {
	var q float64
	err := l.db.Raw(`SELECT COALESCE(SUM(qty_on_hand),0) FROM wms.stocks WHERE tenant_id=? AND factory_id=? AND material_id=?`, tenantID, factoryID, materialID).Scan(&q).Error
	return q, err
}

func (l *localWMS) StockOrderTodos(_ context.Context, tenantID, factoryID int64) (int64, int64, error) {
	var rcv, shp int64
	l.db.Raw(`SELECT COUNT(*) FROM wms.stock_orders WHERE tenant_id=? AND factory_id=? AND status IN ('draft','doing') AND doc_type='rcv'`, tenantID, factoryID).Scan(&rcv)
	l.db.Raw(`SELECT COUNT(*) FROM wms.stock_orders WHERE tenant_id=? AND factory_id=? AND status IN ('draft','doing') AND doc_type='shp'`, tenantID, factoryID).Scan(&shp)
	return rcv, shp, nil
}

type remoteWMS struct {
	baseURL string
	token   string
	hc      *http.Client
}

func (r *remoteWMS) fetch(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Internal-Token", r.token)
	resp, err := r.hc.Do(req)
	if err != nil {
		return fmt.Errorf("core-wms %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("core-wms %s: http %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (r *remoteWMS) StockSums(ctx context.Context, tenantID, factoryID int64) ([]StockSum, error) {
	var out struct {
		Data struct {
			List []StockSum `json:"list"`
		} `json:"data"`
	}
	if err := r.fetch(ctx, fmt.Sprintf("/internal/core/wms/stock-sums?tenantId=%d&factoryId=%d", tenantID, factoryID), &out); err != nil {
		return nil, err
	}
	return out.Data.List, nil
}

func (r *remoteWMS) ReservedSums(ctx context.Context, tenantID, factoryID int64) ([]StockSum, error) {
	var out struct {
		Data struct {
			List []StockSum `json:"list"`
		} `json:"data"`
	}
	if err := r.fetch(ctx, fmt.Sprintf("/internal/core/wms/reserved-sums?tenantId=%d&factoryId=%d", tenantID, factoryID), &out); err != nil {
		return nil, err
	}
	return out.Data.List, nil
}

func (r *remoteWMS) StockSingle(ctx context.Context, tenantID, factoryID, materialID int64) (float64, error) {
	var out struct {
		Data struct {
			Qty float64 `json:"qty"`
		} `json:"data"`
	}
	if err := r.fetch(ctx, fmt.Sprintf("/internal/core/wms/stock-single?tenantId=%d&factoryId=%d&materialId=%d", tenantID, factoryID, materialID), &out); err != nil {
		return 0, err
	}
	return out.Data.Qty, nil
}

func (r *remoteWMS) StockOrderTodos(ctx context.Context, tenantID, factoryID int64) (int64, int64, error) {
	var out struct {
		Data struct {
			Rcv int64 `json:"rcv"`
			Shp int64 `json:"shp"`
		} `json:"data"`
	}
	if err := r.fetch(ctx, fmt.Sprintf("/internal/core/wms/stockorder-todos?tenantId=%d&factoryId=%d", tenantID, factoryID), &out); err != nil {
		return 0, 0, err
	}
	return out.Data.Rcv, out.Data.Shp, nil
}

var (
	wmsOnce sync.Once
	wmsCh   CoreWMS
)

// InitWMS 进程启动装配（main 调用；EM_WMS_URL=远端 core-wms，未设=Local 直连本进程库）。
func InitWMS(db *gorm.DB) {
	wmsOnce.Do(func() {
		if u := os.Getenv("EM_WMS_URL"); u != "" {
			wmsCh = &remoteWMS{baseURL: u, token: os.Getenv("EM_INTERNAL_TOKEN"), hc: &http.Client{Timeout: 5 * time.Second}}
		} else {
			wmsCh = &localWMS{db: db}
		}
	})
}

// WMS 默认通道（须先 InitWMS）。
func WMS() CoreWMS {
	wmsOnce.Do(func() { wmsCh = &localWMS{} })
	return wmsCh
}
