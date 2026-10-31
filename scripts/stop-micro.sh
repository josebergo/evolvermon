#!/usr/bin/env bash
# 停止 Micro 拓扑：业务服务经管理平台 API 停止，ops-admin/gateway 按进程名兜底。
set -uo pipefail
cd "$(dirname "$0")/../backend"
export PYTHONUTF8=1

AT=$(curl -s -X POST -H "Content-Type: application/json" -d '{"username":"admin","password":"Admin@123"}' \
  http://127.0.0.1:8080/api/v1/auth/login 2>/dev/null | python -c "import sys,json;print(json.load(sys.stdin)['data']['accessToken'])" 2>/dev/null)
if [ -n "${AT:-}" ]; then
  for s in scheduler integration core master-data identity; do
    curl -s -X POST -H "Authorization: Bearer $AT" "http://127.0.0.1:8080/api/v1/ops/services/$s/stop" >/dev/null 2>&1
  done
fi
for n in gateway ops-admin identity integration scheduler server master-data; do taskkill //IM $n.exe //F >/dev/null 2>&1; done
rm -rf .pids .pids-*.log
echo "micro 拓扑已停止"
