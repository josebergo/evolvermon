#!/usr/bin/env bash
# Micro 形态端到端验证（Phase 1 验收门槛）：
#   ① 金路径 64 断言（全部流量经网关分流到 identity/core/integration）
#   ② 管理平台验证（verify-ops.sh：注册/健康/启停/审计/告警/故障隔离）
#   ③ 200 并发报工压测经网关，P99 ≤ 48ms（基线 32ms × 1.5）
# 前置：bash scripts/run-micro.sh（默认会重置库）
set -uo pipefail
cd "$(dirname "$0")/.."
BASE="${1:-http://127.0.0.1:8080}"

echo "======== ① 金路径（经网关）========"
PYTHONUTF8=1 bash scripts/verify-golden-path.sh "$BASE" || exit 1

echo ""
echo "======== ② 统一管理平台 ========"
bash scripts/verify-ops.sh "$BASE" || exit 1

echo ""
echo "======== ③ 并发压测（经网关）========"
PYTHONUTF8=1 bash scripts/load-report.sh "$BASE" 200
