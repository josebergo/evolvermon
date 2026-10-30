// Package conv api 层通用小工具（Phase 0.1 域包拆分时自各 api 文件集中迁入）。
package conv

import (
	"fmt"
	"strconv"
	"time"
)

// I64s int64 转字符串。
func I64s(v int64) string { return strconv.FormatInt(v, 10) }

// PtrTime 取时间指针（GORM 更新零值字段用）。
func PtrTime(t time.Time) *time.Time { return &t }

// F64 数值展示格式化。
func F64(f float64) string { return fmt.Sprintf("%g", f) }

// Ts 日期展示（空值占位符）。
func Ts(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "—"
	}
	return t.Format("2006-01-02")
}
