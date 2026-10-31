// Package ops 统一管理平台域（Phase 1.5）：服务注册表 / 健康聚合 / 启停 Driver / 指标抓取 / 告警 / 审计。
package ops

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ServiceDef 注册表条目（registry.yml）。
type ServiceDef struct {
	Name   string            `yaml:"name" json:"name"`
	URL    string            `yaml:"url" json:"url"` // 健康探针基址（含端口）
	Driver string            `yaml:"driver" json:"driver"` // process | compose | none（仅观测）
	Start  string            `yaml:"start" json:"start"`   // process：可执行文件
	Dir    string            `yaml:"dir" json:"dir"`       // process：工作目录（默认 registry 所在目录）
	Args   []string          `yaml:"args" json:"args"`
	Env    map[string]string `yaml:"env" json:"env"`
	ComposeProject string    `yaml:"composeProject" json:"composeProject"` // compose：项目名
	ComposeFile    string    `yaml:"composeFile" json:"composeFile"`       // compose：compose 文件路径
}

type registryFile struct {
	Services []ServiceDef `yaml:"services"`
}

// LoadRegistry 读取 registry.yml（EM_REGISTRY_PATH，默认 ops/registry.yml）。
func LoadRegistry(path string) ([]ServiceDef, error) {
	if path == "" {
		path = os.Getenv("EM_REGISTRY_PATH")
		if path == "" {
			path = "ops/registry.yml"
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	var f registryFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("registry parse: %w", err)
	}
	if len(f.Services) == 0 {
		return nil, fmt.Errorf("registry %s: no services", path)
	}
	return f.Services, nil
}
