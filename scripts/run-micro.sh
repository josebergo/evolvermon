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
  for db in evolvermon evolvermon_erp evolvermon_mes evolvermon_wms evolvermon_identity evolvermon_master evolvermon_integration evolvermon_ops; do
    "$PGSQL/psql.exe" -h 127.0.0.1 -U postgres -c "DROP DATABASE IF EXISTS $db WITH (FORCE);" -c "CREATE DATABASE $db;" >/dev/null
  done
fi

CORE_DSN="host=127.0.0.1 port=5432 user=postgres password=postgres dbname=evolvermon sslmode=disable TimeZone=Asia/Shanghai"
CORE_API="http://127.0.0.1:8082" # core 域内部 API 基址（mono=8082 单进程；split 段覆盖为 8088 core-wms）
export EM_CORE_URL="$CORE_API"   # integration/identity/master-data 继承
# 拓扑判定前置（identity/master-data 的 EM_CORE_URL 依赖）：split 时 core 域 API 归 core-wms
if [ "${EM_TOPO:-mono}" = "split" ]; then
  CORE_API="http://127.0.0.1:8088"
  export EM_CORE_URL="$CORE_API"
fi
# 运行时 registry：split 时删 core 单进程条目（三进程条目观测），mono 时 core url 指向 CORE_API
CORE_API_ENV="$CORE_API" EM_TOPO_ENV="${EM_TOPO:-mono}" python <<'PYRGX'
import io, os
core_api, topo = os.environ['CORE_API_ENV'], os.environ['EM_TOPO_ENV']
drop = ['core'] if topo == 'split' else ['core-erp', 'core-mes', 'core-wms']
lines = io.open('backend/ops/registry.yml', encoding='utf-8', newline='').readlines()
out, skip = [], False
for l in lines:
    t = l.strip()
    if any(t == '- name: ' + d for d in drop):
        skip = True
        continue
    if skip and t.startswith('- name:'):
        skip = False
    if not skip:
        out.append(l)
s = ''.join(out)
if topo != 'split':
    s = s.replace('url: http://127.0.0.1:8082', 'url: ' + core_api, 1)
io.open('backend/ops/registry.runtime.yml', 'w', encoding='utf-8', newline='').write(s)
PYRGX
export EM_REGISTRY_PATH="ops/registry.runtime.yml"  # ops-admin cwd=backend
cd backend
rm -rf .pids
wait_ready() { for i in $(seq 1 30); do curl -sf "$1" >/dev/null 2>&1 && return 0; sleep 1; done; echo "  ✗ $1 未就绪"; return 1; }

echo "== 启动 identity（迁移+种子责任方，直启）=="
EM_DATABASE_DSN="host=127.0.0.1 port=5432 user=postgres password=postgres dbname=evolvermon_identity sslmode=disable TimeZone=Asia/Shanghai" EM_HTTP_ADDR=":8081" EM_INTERNAL_TOKEN="dev-internal-token" EM_CORE_URL="$CORE_API" ./identity.exe > .pids-identity.log 2>&1 &
wait_ready http://127.0.0.1:8081/readyz || { tail -3 .pids-identity.log; exit 1; }

echo "== 启动 ops-admin + gateway（自不管辖） =="
EM_DATABASE_DSN="host=127.0.0.1 port=5432 user=postgres password=postgres dbname=evolvermon_ops sslmode=disable TimeZone=Asia/Shanghai" EM_HTTP_ADDR=":8180" EM_INTERNAL_TOKEN="dev-internal-token" EM_IDENTITY_URL="http://127.0.0.1:8081" ./ops-admin.exe > .pids-ops-admin.log 2>&1 &
EM_DATABASE_DSN="host=127.0.0.1 port=5432 user=postgres password=postgres dbname=evolvermon_master sslmode=disable TimeZone=Asia/Shanghai" EM_HTTP_ADDR=":8085" EM_INTERNAL_TOKEN="dev-internal-token" EM_IDENTITY_URL="http://127.0.0.1:8081" EM_CORE_URL="$CORE_API" ./master-data.exe > .pids-master-data.log 2>&1 &
wait_ready http://127.0.0.1:8180/readyz
wait_ready http://127.0.0.1:8085/readyz || { tail -3 .pids-master-data.log; exit 1; } || { tail -3 .pids-ops-admin.log; exit 1; }

CORE_DSN="host=127.0.0.1 port=5432 user=postgres password=postgres dbname=evolvermon sslmode=disable TimeZone=Asia/Shanghai"
CORE_ENV_EXTRA="EM_IDENTITY_URL=http://127.0.0.1:8081 EM_INTERNAL_TOKEN=dev-internal-token EM_MASTER_URL=http://127.0.0.1:8085"
if [ "${EM_TOPO:-mono}" = "split" ]; then
  echo "== split 拓扑：core-erp/mes/wms 三进程 =="
  go build -o core-erp.exe ./cmd/core-erp && go build -o core-mes.exe ./cmd/core-mes && go build -o core-wms.exe ./cmd/core-wms || exit 1
  EM_DATABASE_DSN="host=127.0.0.1 port=5432 user=postgres password=postgres dbname=evolvermon_erp sslmode=disable TimeZone=Asia/Shanghai" EM_HTTP_ADDR=":8086" EM_MIGRATE=1 EM_SEED_DEMO=false EM_MES_URL="http://127.0.0.1:8087" EM_WMS_URL="http://127.0.0.1:8088" EM_IDENTITY_URL="http://127.0.0.1:8081" EM_INTERNAL_TOKEN="dev-internal-token" EM_MASTER_URL="http://127.0.0.1:8085" ./core-erp.exe > .pids-core-erp.log 2>&1 &
  sleep 2
  EM_DATABASE_DSN="host=127.0.0.1 port=5432 user=postgres password=postgres dbname=evolvermon_mes sslmode=disable TimeZone=Asia/Shanghai" EM_HTTP_ADDR=":8087" EM_MIGRATE=1 EM_SEED_DEMO=true EM_ERP_URL="http://127.0.0.1:8086" EM_WMS_URL="http://127.0.0.1:8088" EM_IDENTITY_URL="http://127.0.0.1:8081" EM_INTERNAL_TOKEN="dev-internal-token" EM_MASTER_URL="http://127.0.0.1:8085" ./core-mes.exe > .pids-core-mes.log 2>&1 &
  sleep 2
  EM_DATABASE_DSN="host=127.0.0.1 port=5432 user=postgres password=postgres dbname=evolvermon_wms sslmode=disable TimeZone=Asia/Shanghai" EM_HTTP_ADDR=":8088" EM_ERP_URL="http://127.0.0.1:8086" EM_MES_URL="http://127.0.0.1:8087" EM_MIGRATE=1 EM_IDENTITY_URL="http://127.0.0.1:8081" EM_INTERNAL_TOKEN="dev-internal-token" EM_MASTER_URL="http://127.0.0.1:8085" ./core-wms.exe > .pids-core-wms.log 2>&1 &
  # 网关分流三上游 + core 指向 core-wms（兜底/内部 API）
  export EM_ERP_URL="http://127.0.0.1:8086" EM_MES_URL="http://127.0.0.1:8087" EM_WMS_URL="http://127.0.0.1:8088" EM_CORE_URL="http://127.0.0.1:8088"
  TOPO_SERVICES=""  # 三进程已直启（registry 提供观测条目）
else
  TOPO_SERVICES="core"
fi
EM_MASTER_URL="http://127.0.0.1:8085" ./gateway.exe > .pids-gateway.log 2>&1 &
wait_ready http://127.0.0.1:8080/healthz || { tail -3 .pids-gateway.log; exit 1; }

echo "== 经管理平台启动 $TOPO_SERVICES/integration/scheduler =="
AT=$(curl -s -X POST -H "Content-Type: application/json" -d '{"username":"admin","password":"Admin@123"}'   http://127.0.0.1:8080/api/v1/auth/login | python -c "import sys,json;print(json.load(sys.stdin)['data']['accessToken'])" 2>/dev/null)
[ -n "$AT" ] || { echo "登录失败"; exit 1; }
for s in master-data $TOPO_SERVICES integration scheduler; do
  r=$(curl -s -X POST -H "Authorization: Bearer $AT" "http://127.0.0.1:8080/api/v1/ops/services/$s/start")
  echo "$r" | python -c "import sys,json;d=json.load(sys.stdin);raise SystemExit(0 if d.get('code')==0 else 1)"     && echo "  ✓ $s 已启动（经 ops-admin process driver）" || { echo "  ✗ $s 启动失败: $r"; exit 1; }
done

echo "== 等待业务服务就绪 =="
ok=1
[ "$TOPO_SERVICES" != "core" ] && for u3 in http://127.0.0.1:8086/readyz http://127.0.0.1:8087/readyz http://127.0.0.1:8088/readyz; do wait_ready "$u3" || ok=0; done
WAIT_URLS="http://127.0.0.1:8083/readyz http://127.0.0.1:8084/readyz"
[ "${EM_TOPO:-mono}" != "split" ] && WAIT_URLS="http://127.0.0.1:8082/readyz $WAIT_URLS"
for u in http://127.0.0.1:8081/readyz $WAIT_URLS; do
  wait_ready "$u" || ok=0
done
[ $ok = 1 ] && echo "拓扑就绪：master-data:8085 gateway:8080 identity:8081 core:8082 integration:8083 scheduler:8084 ops-admin:8180" || exit 1
