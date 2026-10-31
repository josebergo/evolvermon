// Phase 0.5：schema 归属静态检查（TestNoCrossSchemaDependency 的落地形态）。
// 三道闸门：
//  1. TestSchemaMapComplete   —— data.Migrate 登记的每张表必须有 schema 归属，映射无 stale 项
//  2. TestRawSQLSchemaQualified —— 原生 SQL（Raw/Joins/字符串 SQL）引用业务表必须 schema 限定
//  3. TestMigration0002Coverage —— 升级迁移必须覆盖全部业务表（存量库 public → 域 schema）
package data

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gorm.io/gorm/schema"

	"github.com/evolvermon/evolvermon/internal/model"
)

// migratedTypes 从 data.Migrate 源码提取 &model.X{} 清单（与 AutoMigrate 登记强绑定）。
func migratedTypes(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("db.go")
	if err != nil {
		t.Fatalf("read db.go: %v", err)
	}
	re := regexp.MustCompile(`&model\.(\w+)\{`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
		out = append(out, m[1])
	}
	if len(out) < 40 {
		t.Fatalf("db.go 解析异常：仅提取到 %d 个模型", len(out))
	}
	return out
}

func TestSchemaMapComplete(t *testing.T) {
	ns := schema.NamingStrategy{}
	mapped := map[string]bool{}
	for tbl := range model.TableSchemaForTest() {
		mapped[tbl] = true
	}
	for _, typ := range migratedTypes(t) {
		table := ns.TableName(typ)
		if model.SchemaOf(table) == "" {
			t.Errorf("模型 %s（表 %s）未登记 schema 归属（internal/model/schema.go）", typ, table)
		}
		delete(mapped, table)
	}
	for stale := range mapped {
		t.Errorf("schema 映射存在已不再迁移的表 %s（stale，需同步清理）", stale)
	}
}

var sqlTableRe = regexp.MustCompile(`(?i)\b(FROM|JOIN|UPDATE|INTO)\s+([a-z_][a-z0-9_]*)`)

func TestRawSQLSchemaQualified(t *testing.T) {
	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(raw), "\n") {
			s := strings.TrimSpace(line)
			if strings.HasPrefix(s, "//") {
				continue
			}
			for _, m := range sqlTableRe.FindAllStringSubmatch(s, -1) {
				tok := m[2]
				if model.SchemaOf(tok) == "" {
					continue // 非业务表（别名/元表）或已限定（带 . 时正则只捕获到 schema 段则不在映射内）
				}
				// 已限定形如 FROM mes.work_orders —— 正则会把 work_orders 前面的 mes 单独看吗？
				// 不会：FROM 后第一个 token 是 mes（不在表映射），work_orders 不再跟在关键字后。
				// 因此这里 tok 命中映射即说明它直接跟在 FROM/JOIN/UPDATE/INTO 后而未限定。
				t.Errorf("%s:%d 原生 SQL 引用业务表 %s 未 schema 限定（应为 %s.%s）",
					filepath.ToSlash(path), i+1, tok, model.SchemaOf(tok), tok)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMigration0002Coverage(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", "0002_domain_schemas.up.sql"))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(raw)
	for tbl, sc := range model.TableSchemaForTest() {
		want := "ALTER TABLE IF EXISTS public." + tbl + " SET SCHEMA " + sc + ";"
		if !strings.Contains(sql, want) {
			t.Errorf("迁移 0002 缺少 %s（期望语句：%s）", tbl, want)
		}
	}
}
