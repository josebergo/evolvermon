#!/usr/bin/env bash
# Micro 形态本地编排（Phase 1.7）：gateway/identity/core/integration/scheduler/ops-admin
# 正统形态：脚本只手起 ops-admin + gateway（driver=none 自不管辖），其余服务
# 一律经统一管理平台 API 启停（process driver，Windows pid 由驱动自己记录）。
# 用法：bash scripts/run-micro.sh   （默认重置数据库；EM_KEEP_DB=1 跳过）
set -uo pipefail
cd "$(dirname "$0")/.."
ROOT=$(pwd)
PGSQL="$ROOT/.pgsql/pgsql/bin"
export PYTHONUTF8=1

echo "== 构建 =="
(cd backend && go build -o gateway.exe ./cmd/gateway && go build -o identity.exe ./cmd/identity   && go build -o server.exe ./cmd/server && go build -o integration.exe ./cmd/integration   && go build -o scheduler.exe ./cmd/scheduler && go build -o ops-admin.exe ./cmd/ops-admin && go build -o master-data.exe ./cmd/master-data) || exit 1

if ! "$PGSQL/pg_isready.exe" -h 127.0.0.1 -p 5432 -q 2>/dev/null; then
  "$PGSQL/pg_ctl.exe" -D "$ROOT/.pgsql/data" -l "$ROOT/.pgsql/pg.log" start >/dev/null 2>&1
  sleep 2
fi

if [ "${EM_KEEP_DB:-0}" != "1" ]; then
  echo "== 重置数据库 =="
  "$PGSQL/psql.exe" -h 127.0.0.1 -U postgres -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='evolvermon' AND pid<>pg_backend_pid();" >/dev/null 2>&1
  "$PGSQL/psql.exe" -h 127.0.0.1 -U postgres -c "DROP DATABASE IF EXISTS evolvermon WITH (FORCE);" -c "CREATE DATABASE evolvermon;" >/dev/null
fi

cd backend
rm -rf .pids
wait_ready() { for i in $(seq 1 30); do curl -sf "$1" >/dev/null 2>&1 && return 0; sleep 1; done; echo "  ✗ $1 未就绪"; return 1; }

echo "== 启动 identity（迁移+种子责任方，直启）=="
EM_HTTP_ADDR=":8081" ./identity.exe > .pids-identity.log 2>&1 &
wait_ready http://127.0.0.1:8081/readyz || { tail -3 .pids-identity.log; exit 1; }

echo "== 启动 ops-admin + gateway（自不管辖） =="
EM_HTTP_ADDR=":8180" ./ops-admin.exe > .pids-ops-admin.log 2>&1 &
EM_HTTP_ADDR=":8085" EM_INTERNAL_TOKEN="dev-internal-token" EM_MIGRATE=0 ./master-data.exe > .pids-master-data.log 2>&1 &
EM_MASTER_URL="http://127.0.0.1:8085" ./gateway.exe > .pids-gateway.log 2>&1 &
wait_ready http://127.0.0.1:8180/readyz
wait_ready http://127.0.0.1:8085/readyz || { tail -3 .pids-master-data.log; exit 1; } || { tail -3 .pids-ops-admin.log; exit 1; }
wait_ready http://127.0.0.1:8080/healthz || { tail -3 .pids-gateway.log; exit 1; }

echo "== 经管理平台启动 core/integration/scheduler =="
AT=$(curl -s -X POST -H "Content-Type: application/json" -d '{"username":"admin","password":"Admin@123"}'   http://127.0.0.1:8080/api/v1/auth/login | python -c "import sys,json;print(json.load(sys.stdin)['data']['accessToken'])" 2>/dev/null)
[ -n "$AT" ] || { echo "登录失败"; exit 1; }
for s in master-data core integration scheduler; do
  r=$(curl -s -X POST -H "Authorization: Bearer $AT" "http://127.0.0.1:8080/api/v1/ops/services/$s/start")
  echo "$r" | python -c "import sys,json;d=json.load(sys.stdin);raise SystemExit(0 if d.get('code')==0 else 1)"     && echo "  ✓ $s 已启动（经 ops-admin process driver）" || { echo "  ✗ $s 启动失败: $r"; exit 1; }
done

echo "== 等待业务服务就绪 =="
ok=1
for u in http://127.0.0.1:8081/readyz http://127.0.0.1:8082/readyz http://127.0.0.1:8083/readyz http://127.0.0.1:8084/readyz; do
  wait_ready "$u" || ok=0
done
[ $ok = 1 ] && echo "拓扑就绪：master-data:8085 gateway:8080 identity:8081 core:8082 integration:8083 scheduler:8084 ops-admin:8180" || exit 1
