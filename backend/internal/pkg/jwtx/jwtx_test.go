package jwtx

import "testing"

func TestTokenRoundTrip(t *testing.T) {
	m := New("test-secret", "evolvermon", 2, 7)
	access, err := m.IssueAccess(42, 7, 1, false)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	cl, err := m.Parse(access)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cl.UserID != 42 || cl.TenantID != 7 || cl.FactoryID != 1 || cl.Kind != "access" {
		t.Fatalf("claims mismatch: %+v", cl)
	}
	// refresh 不能当 access 用（认证中间件同样校验 Kind）
	refresh, _ := m.IssueRefresh(42, 7, 1, false)
	if rc, err := m.Parse(refresh); err != nil || rc.Kind != "refresh" {
		t.Fatalf("refresh kind: %v %+v", err, rc)
	}
	// 错误签名拒绝
	m2 := New("other-secret", "evolvermon", 2, 7)
	if _, err := m2.Parse(access); err == nil {
		t.Fatal("token signed with different secret should be rejected")
	}
}
