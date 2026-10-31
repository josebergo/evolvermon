// Package client — identity 读/写通道（Phase 2 第二批）：单号 / 审计 / 认证上下文。
// EM_IDENTITY_URL 配置 → 远程（拆库后唯一通道）；否则空=未启用（调用方走本地直连）。
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// IdentityClient identity 内部 API 客户端（尽力而为语义按方法区分）。
type IdentityClient struct {
	baseURL string
	token   string
	hc      *http.Client
}

var (
	identityOnce  sync.Once
	identityAgent *IdentityClient
)

// Identity 按环境装配单例（EM_IDENTITY_URL 未配置返回 nil）。
func Identity() *IdentityClient {
	identityOnce.Do(func() {
		if u := os.Getenv("EM_IDENTITY_URL"); u != "" {
			identityAgent = &IdentityClient{
				baseURL: u, token: os.Getenv("EM_INTERNAL_TOKEN"),
				hc: &http.Client{Timeout: 5 * time.Second},
			}
		}
	})
	return identityAgent
}

// DocNo 远程取单号（强一致：服务端行锁）。
func (c *IdentityClient) DocNo(tenantID int64, docType string) (string, error) {
	var out struct {
		Data struct {
			DocNo string `json:"docNo"`
		} `json:"data"`
	}
	body, _ := json.Marshal(map[string]any{"tenantId": tenantID, "docType": docType})
	if err := c.post("/internal/identity/docno", body, &out); err != nil {
		return "", err
	}
	return out.Data.DocNo, nil
}

// Audit 远程审计（尽力而为：失败仅记日志，绝不阻塞业务）。
func (c *IdentityClient) Audit(payload map[string]any) {
	body, _ := json.Marshal(payload)
	var out any
	_ = c.post("/internal/identity/audit", body, &out)
}

// AuthContext 远程认证上下文。
func (c *IdentityClient) AuthContext(userID int64) (*AuthContextDTO, error) {
	var out struct {
		Data AuthContextDTO `json:"data"`
	}
	if err := c.get(fmt.Sprintf("/internal/identity/auth-context?userId=%d", userID), &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// AuthContextDTO 认证上下文 DTO（与 middleware.AuthCtx 字段对齐）。
type AuthContextDTO struct {
	UserID    int64    `json:"userId"`
	TenantID  int64    `json:"tenantId"`
	FactoryID int64    `json:"factoryId"`
	Username  string   `json:"username"`
	IsSuper   bool     `json:"isSuper"`
	Perms     []string `json:"perms"`
	DataScope string `json:"dataScope"`
}

// FactoryLite 工厂精简视图。
type FactoryLite struct {
	ID       int64  `json:"id"`
	TenantID int64  `json:"tenantId"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Status   int    `json:"status"`
}

// Factories 活跃工厂列表。
func (c *IdentityClient) Factories() ([]FactoryLite, error) {
	var out struct {
		Data struct {
			List []FactoryLite `json:"list"`
		} `json:"data"`
	}
	if err := c.get("/internal/identity/factories", &out); err != nil {
		return nil, err
	}
	return out.Data.List, nil
}

// PutParam 写系统参数（尽力而为）。
func (c *IdentityClient) PutParam(tenantID, factoryID int64, key, value string) {
	body, _ := json.Marshal(map[string]any{"tenantId": tenantID, "factoryId": factoryID, "key": key, "value": value})
	var out any
	_ = c.post("/internal/identity/param", body, &out)
}

func (c *IdentityClient) post(path string, body []byte, out any) error {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", c.token)
	return c.do(req, out)
}

func (c *IdentityClient) get(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Internal-Token", c.token)
	return c.do(req, out)
}

func (c *IdentityClient) do(req *http.Request, out any) error {
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("identity %s: %w", req.URL.Path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("identity %s: http %d", req.URL.Path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// FactoryCheck core 单据开放数（identity 工厂停用校验用；>0 禁止停用）。
func FactoryCheck(coreURL, token string, factoryID int64) (int64, error) {
	hc := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodGet,
		fmt.Sprintf("%s/internal/core/factory-check?factoryId=%d", coreURL, factoryID), nil)
	req.Header.Set("X-Internal-Token", token)
	resp, err := hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("core factory-check: http %d", resp.StatusCode)
	}
	var out struct {
		Data struct {
			OpenDocs int64 `json:"openDocs"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.Data.OpenDocs, nil
}

// boolParamCache 参数进程内缓存（5s：过账高频读取 + 参数变更快速生效）。
var boolParamCache = struct {
	sync.Mutex
	m map[string]paramEntry
}{m: map[string]paramEntry{}}

// boolParamTTL 缓存时长（测试可缩短）。
const boolParamTTL = 5 * time.Second

type paramEntry struct {
	val bool
	at  time.Time
}

// BoolParam 远程读布尔参数（60s 缓存；失败按 false，与未配置语义一致）。
func (c *IdentityClient) BoolParam(tenantID int64, key string) bool {
	ck := fmt.Sprintf("%d:%s", tenantID, key)
	boolParamCache.Lock()
	if e, ok := boolParamCache.m[ck]; ok && time.Since(e.at) < boolParamTTL {
		boolParamCache.Unlock()
		return e.val
	}
	boolParamCache.Unlock()
	var out struct {
		Data struct {
			Value string `json:"value"`
		} `json:"data"`
	}
	val := false
	if err := c.get(fmt.Sprintf("/internal/identity/param?tenantId=%d&key=%s", tenantID, key), &out); err == nil {
		val = out.Data.Value == "true" || out.Data.Value == `"true"`
	}
	boolParamCache.Lock()
	boolParamCache.m[ck] = paramEntry{val: val, at: time.Now()}
	boolParamCache.Unlock()
	return val
}
