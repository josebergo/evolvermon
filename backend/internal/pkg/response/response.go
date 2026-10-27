// Package response 统一 API 响应包（api-design.md §0）。
package response

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	OK            = 0
	ErrAuth       = 1000
	ErrForbidden  = 1003
	ErrParam      = 2000
	ErrStockOut   = 3001
	ErrBadState   = 3002
	ErrVersion    = 3003
	ErrBomCycle   = 3004
	ErrIdempotent = 3005
	ErrBiz        = 3000
	ErrSystem     = 4000
)

// BizError 业务错误，service 层返回；handler 统一翻译。
type BizError struct {
	Code int
	Msg  string
	HTTP int
}

func (e *BizError) Error() string { return e.Msg }

func NewBiz(code int, msg string) *BizError {
	httpc := http.StatusBadRequest
	switch code {
	case ErrAuth:
		httpc = http.StatusUnauthorized
	case ErrForbidden:
		httpc = http.StatusForbidden
	case ErrVersion, ErrBadState, ErrIdempotent:
		httpc = http.StatusConflict
	case ErrParam:
		httpc = http.StatusUnprocessableEntity
	}
	return &BizError{Code: code, Msg: msg, HTTP: httpc}
}

type Body struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    any         `json:"data,omitempty"`
	TraceID string      `json:"traceId,omitempty"`
}

func OK2(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Body{Code: 0, Message: "ok", Data: data, TraceID: traceID(c)})
}

func Page(c *gin.Context, list any, total int64) {
	OK2(c, gin.H{"list": list, "total": total})
}

func Fail(c *gin.Context, err error) {
	var be *BizError
	if errors.As(err, &be) {
		c.JSON(be.HTTP, Body{Code: be.Code, Message: be.Msg, TraceID: traceID(c)})
		return
	}
	c.JSON(http.StatusInternalServerError, Body{Code: ErrSystem, Message: err.Error(), TraceID: traceID(c)})
}

func FailMsg(c *gin.Context, code int, msg string) {
	c.JSON(http.StatusBadRequest, Body{Code: code, Message: msg})
}

func traceID(c *gin.Context) string {
	if v, ok := c.Get("trace_id"); ok {
		return v.(string)
	}
	return ""
}
