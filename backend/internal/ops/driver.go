// 启停 Driver（Phase 1.5）：端口-适配器，与 infra 同风格。
// process：本机进程拉起/终止（PID 文件跟踪；Lite/裸机多进程形态，verify-ops.sh 实测）。
// compose：docker compose 服务启停（Micro 容器形态；本机无 Docker，CI/容器环境验证）。
// none：仅观测不可控（gateway 自身等）。
package ops

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Driver 服务生命周期控制接口。
type Driver interface {
	Start(ctx context.Context, def ServiceDef) error
	Stop(ctx context.Context, def ServiceDef) error
	Restart(ctx context.Context, def ServiceDef) error
	Kind() string
}

// NewDriver 按注册表 driver 字段装配；未知类型 fail-fast。
func NewDriver(kind string) Driver {
	switch kind {
	case "process":
		return &ProcessDriver{}
	case "compose":
		return &ComposeDriver{}
	case "none", "":
		return &NoneDriver{}
	default:
		return &BadDriver{kind: kind}
	}
}

// ---- process ----

type ProcessDriver struct{}

func pidFile(def ServiceDef) string {
	dir := def.Dir
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, ".pids", def.Name+".pid")
}

func (d *ProcessDriver) Start(ctx context.Context, def ServiceDef) error {
	if pid, ok := readPid(def); ok && processAlive(pid) {
		return fmt.Errorf("service %s already running (pid %d)", def.Name, pid)
	}
	cmd := exec.Command(def.Start, def.Args...)
	if def.Dir != "" {
		cmd.Dir = def.Dir
	}
	cmd.Env = append(os.Environ(), envList(def.Env)...)
	logPath := filepath.Join(filepath.Dir(pidFile(def)), def.Name+".log")
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd.Stdout, cmd.Stderr = f, f
	if err := cmd.Start(); err != nil {
		return err
	}
	return os.WriteFile(pidFile(def), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
}

func (d *ProcessDriver) Stop(ctx context.Context, def ServiceDef) error {
	pid, ok := readPid(def)
	if !ok {
		return fmt.Errorf("service %s: no pid file", def.Name)
	}
	if !processAlive(pid) {
		_ = os.Remove(pidFile(def))
		return nil
	}
	if err := killTree(pid); err != nil {
		return err
	}
	// 等待退出（最多 5s）
	for i := 0; i < 50 && processAlive(pid); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if processAlive(pid) {
		return fmt.Errorf("service %s pid %d did not exit", def.Name, pid)
	}
	return os.Remove(pidFile(def))
}

func (d *ProcessDriver) Restart(ctx context.Context, def ServiceDef) error {
	if _, ok := readPid(def); ok && processAlive(0) {
		_ = d.Stop(ctx, def)
	}
	return d.Start(ctx, def)
}

func (*ProcessDriver) Kind() string { return "process" }

// ---- compose ----

type ComposeDriver struct{}

func (d *ComposeDriver) compose(ctx context.Context, def ServiceDef, verb string) error {
	args := []string{"compose"}
	if def.ComposeFile != "" {
		args = append(args, "-f", def.ComposeFile)
	}
	if def.ComposeProject != "" {
		args = append(args, "-p", def.ComposeProject)
	}
	args = append(args, verb, def.Name)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose %s %s: %v: %s", verb, def.Name, err, out)
	}
	return nil
}

func (d *ComposeDriver) Start(ctx context.Context, def ServiceDef) error   { return d.compose(ctx, def, "up", ) }
func (d *ComposeDriver) Stop(ctx context.Context, def ServiceDef) error    { return d.compose(ctx, def, "stop") }
func (d *ComposeDriver) Restart(ctx context.Context, def ServiceDef) error { return d.compose(ctx, def, "restart") }
func (*ComposeDriver) Kind() string                                       { return "compose" }

// ---- none / bad ----

type NoneDriver struct{}

func (*NoneDriver) Start(context.Context, ServiceDef) error   { return errNotControllable }
func (*NoneDriver) Stop(context.Context, ServiceDef) error    { return errNotControllable }
func (*NoneDriver) Restart(context.Context, ServiceDef) error { return errNotControllable }
func (*NoneDriver) Kind() string                              { return "none" }

type BadDriver struct{ kind string }

func (d *BadDriver) Start(context.Context, ServiceDef) error {
	return fmt.Errorf("unknown driver kind: %s", d.kind)
}
func (d *BadDriver) Stop(context.Context, ServiceDef) error    { return fmt.Errorf("unknown driver kind: %s", d.kind) }
func (d *BadDriver) Restart(context.Context, ServiceDef) error { return fmt.Errorf("unknown driver kind: %s", d.kind) }
func (d *BadDriver) Kind() string                              { return d.kind }

var errNotControllable = fmt.Errorf("driver=none：该服务仅观测不可控（如 gateway/ops-admin 自身）")

// ---- 公共小工具 ----

func envList(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

func readPid(def ServiceDef) (int, bool) {
	raw, err := os.ReadFile(pidFile(def))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		// Windows 无 Signal(0) 探活：tasklist 精确匹配 PID
		out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/FO", "CSV", "/NH").Output()
		return err == nil && strings.Contains(string(out), strconv.Itoa(pid))
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func killTree(pid int) error {
	if runtime.GOOS == "windows" {
		return exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
