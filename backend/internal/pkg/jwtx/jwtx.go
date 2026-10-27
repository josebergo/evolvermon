// Package jwtx 签发/解析 JWT（RS256 留位，M1 HMAC-SHA256）。
package jwtx

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID    int64   `json:"uid"`
	TenantID  int64   `json:"tid"`
	FactoryID int64   `json:"fid"`
	IsSuper   bool    `json:"sup,omitempty"`
	Kind      string  `json:"kind"` // access | refresh
	jwt.RegisteredClaims
}

type Manager struct {
	secret   []byte
	issuer   string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func New(secret, issuer string, accessHour, refreshDay int) *Manager {
	return &Manager{
		secret:     []byte(secret),
		issuer:     issuer,
		accessTTL:  time.Duration(accessHour) * time.Hour,
		refreshTTL: time.Duration(refreshDay) * 24 * time.Hour,
	}
}

func (m *Manager) issue(uid, tid, fid int64, isSuper bool, kind string, ttl time.Duration) (string, error) {
	now := time.Now()
	cl := Claims{
		UserID: uid, TenantID: tid, FactoryID: fid, IsSuper: isSuper, Kind: kind,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   kind,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, cl).SignedString(m.secret)
}

func (m *Manager) IssueAccess(uid, tid, fid int64, isSuper bool) (string, error) {
	return m.issue(uid, tid, fid, isSuper, "access", m.accessTTL)
}

func (m *Manager) IssueRefresh(uid, tid, fid int64, isSuper bool) (string, error) {
	return m.issue(uid, tid, fid, isSuper, "refresh", m.refreshTTL)
}

func (m *Manager) Parse(tokenStr string) (*Claims, error) {
	t, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		return m.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !t.Valid {
		return nil, errors.New("invalid token")
	}
	cl, ok := t.Claims.(*Claims)
	if !ok {
		return nil, errors.New("invalid claims")
	}
	return cl, nil
}
