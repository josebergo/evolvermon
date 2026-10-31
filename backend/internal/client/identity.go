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
