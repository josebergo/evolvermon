// Package data：版本化迁移（NFR-021）。
// 策略：首次部署以 AutoMigrate 固化 baseline=1；此后增量变更放 backend/migrations/NNNN_name.up.sql，
// 启动时按版本顺序执行并记录 schema_migrations。回滚：提供对应 .down.sql 人工执行（向前兼容两版本）。
package data

import (
	"strings"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"gorm.io/gorm"

	"evolvermon/backend/internal/model"
)

type schemaMigration struct {
	Version    int    `gorm:"primaryKey"`
	Name       string `gorm:"size:128"`
	AppliedAt  string `gorm:"default:now()"`
}

func (schemaMigration) TableName() string { return "schema_migrations" }

// MigrateVersioned 执行版本化迁移，返回已应用到的版本号。
func MigrateVersioned(db *gorm.DB, profile ...string) (int, error) {
	prof := "all"
	if len(profile) > 0 {
		prof = profile[0]
	}
	if err := db.AutoMigrate(&schemaMigration{}); err != nil {
		return 0, err
	}
	var last schemaMigration
	db.Order("version DESC").First(&last)

	if last.Version == 0 {
		// 基线：全量建表（先建域 schema，Phase 0.2）
		if err := EnsureSchemas(db); err != nil {
			return 0, err
		}
		if err := MigrateProfile(db, prof); err != nil {
			return 0, fmt.Errorf("baseline migrate: %w", err)
		}
		db.Create(&schemaMigration{Version: 1, Name: "baseline(auto)"})
		last = schemaMigration{Version: 1}
	}

	files, err := loadUpMigrations()
	if err != nil {
		return last.Version, err
	}
	for _, m := range files {
		if m.version <= last.Version {
			continue
		}
		if err := execSQLFile(db, m.path); err != nil {
			return last.Version, fmt.Errorf("migration %s: %w", m.name, err)
		}
		db.Create(&schemaMigration{Version: m.version, Name: m.name})
		last.Version = m.version
	}
	return last.Version, nil
}

type migFile struct {
	version int
	name    string
	path    string
}

var migNameRe = regexp.MustCompile(`^(\d{4})_(.+)\.up\.sql$`)

func loadUpMigrations() ([]migFile, error) {
	dir := os.Getenv("EM_MIGRATIONS_DIR")
	if dir == "" {
		dir = "migrations"
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []migFile
	for _, e := range entries {
		m := migNameRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		v, _ := strconv.Atoi(m[1])
		out = append(out, migFile{version: v, name: m[2], path: filepath.Join(dir, e.Name())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// execSQLFile 按分号切分逐条执行（约定：文件为普通 DDL/DML，不包含函数体等含分号的复合语句）。
func execSQLFile(db *gorm.DB, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, stmt := range splitStatements(string(raw)) {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("stmt %.60s…: %w", stmt, err)
		}
	}
	return nil
}

func splitStatements(sql string) []string {
	var out []string
	cur := ""
	inComment := false
	for _, line := range regexp.MustCompile(`\r?\n`).Split(sql, -1) {
		// 块注释 /* */（跨行状态）
		if inComment {
			if idx := strings.Index(line, "*/"); idx >= 0 {
				line = line[idx+2:]
				inComment = false
			} else {
				continue
			}
		}
		// 行注释 --（含行首）
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		// 行内未闭合块注释
		if idx := strings.Index(line, "/*"); idx >= 0 {
			line = line[:idx]
			inComment = true
		}
		cur += line + "\n"
	}
	for _, stmt := range strings.Split(cur, ";") {
		if s := strings.TrimSpace(stmt); s != "" {
			out = append(out, s)
		}
	}
	return out
}


var _ = model.Tenant{} // 保持 model 引用（baseline 复用 Migrate）