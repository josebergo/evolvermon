// Package client — CoreWMSWriter 写通道（Phase 3 ③A(2)）：mes/erp 建出入库单经此；
// Local=直调 wapi.CreateOrder（共享库事务内语义），Remote=HTTP /internal/core/wms/create-order（EM_WMS_URL）。
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/evolvermon/evolvermon/internal/wms/wapi"
)

// WMSWriter wms 写通道。
type WMSWriter interface {
	CreateOrder(req wapi.CreateReq) (*wapi.CreateResult, error)
	Reserve(req wapi.ReserveReq) error
	Release(req wapi.ReleaseReq) error
}

type localWriter struct{ db *gorm.DB }

func (l *localWriter) CreateOrder(req wapi.CreateReq) (*wapi.CreateResult, error) {
	return wapi.CreateOrder(l.db, req)
}

type remoteWriter struct {
	base, token string
	hc          *http.Client
}

func (r *remoteWriter) CreateOrder(req wapi.CreateReq) (*wapi.CreateResult, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequest(http.MethodPost, r.base+"/internal/core/wms/create-order", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Internal-Token", r.token)
	resp, err := r.hc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("wms create-order: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("wms create-order: http %d", resp.StatusCode)
	}
	var out struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    wapi.CreateResult `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("wms create-order: %s", out.Message)
	}
	return &out.Data, nil
}

func (l *localWriter) Reserve(req wapi.ReserveReq) error { return wapi.Reserve(l.db, req) }
func (l *localWriter) Release(req wapi.ReleaseReq) error { return wapi.Release(l.db, req) }

func (r *remoteWriter) post(path string, in any) error {
	body, _ := json.Marshal(in)
	req, err := http.NewRequest(http.MethodPost, r.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", r.token)
	resp, err := r.hc.Do(req)
	if err != nil {
		return fmt.Errorf("wms %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("wms %s: http %d", path, resp.StatusCode)
	}
	var out struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Code != 0 {
		return fmt.Errorf("wms %s: %s", path, out.Message)
	}
	return nil
}

func (r *remoteWriter) Reserve(req wapi.ReserveReq) error { return r.post("/internal/core/wms/reserve", req) }
func (r *remoteWriter) Release(req wapi.ReleaseReq) error { return r.post("/internal/core/wms/release", req) }

var (
	writerOnce sync.Once
	writerCh   WMSWriter
)

// InitWMSWriter 进程启动装配（EM_WMS_URL=远端 core-wms；未设=Local 直连本进程库）。
func InitWMSWriter(db *gorm.DB) {
	writerOnce.Do(func() {
		if u := os.Getenv("EM_WMS_URL"); u != "" {
			writerCh = &remoteWriter{base: u, token: os.Getenv("EM_INTERNAL_TOKEN"), hc: &http.Client{Timeout: 10 * time.Second}}
		} else {
			writerCh = &localWriter{db: db}
		}
	})
}

// Writer 默认写通道（须先 Init）。
func Writer() WMSWriter {
	writerOnce.Do(func() { writerCh = &localWriter{} })
	return writerCh
}
