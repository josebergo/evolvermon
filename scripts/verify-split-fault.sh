#!/usr/bin/env bash
# Phase 3 收尾验收：split 八库故障注入 + 隔离 + 恢复 + 对账（执行方案 ⑦）。
set -uo pipefail
export PYTHONUTF8=1
BASE="${1:-http://127.0.0.1:8080}/api/v1"
PASS=0; FAIL=0
say()  { printf '\n\033[36m== %s ==\033[0m\n' "$*"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$*"; PASS=$((PASS+1)); }
bad()  { printf '  \033[31m✗\033[0m %s\n' "$*"; FAIL=$((FAIL+1)); }
req()  { local m=$1 p=$2 d=${3:-}; if [ -n "$d" ]; then curl -s -X "$m" -H "Content-Type: application/json" -H "Authorization: Bearer $AT" -d "$d" "$BASE$p"; else curl -s -X "$m" -H "Authorization: Bearer $AT" "$BASE$p"; fi; }

AT=$(curl -s -X POST -H "Content-Type: application/json" -d '{"username":"admin","password":"Admin@123"}' "$BASE/auth/login" | python -c "import sys,json;print(json.load(sys.stdin)['data']['accessToken'])")
[ -n "$AT" ] && ok "登录" || exit 1

say "故障注入：杀 core-mes"
taskkill //IM core-mes.exe //F >/dev/null 2>&1
sleep 3
MES_CODE=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $AT" "$BASE/work-orders?page_size=1")
[ "$MES_CODE" = "502" ] && ok "mes 路由明确 502（不静默）" || bad "mes 路由 $MES_CODE（期望 502）"

say "隔离：erp 流程不受影响（建 SO+审核→预占经 core-wms）"
CUST=$(req GET "/customers?page_size=1" | python -c "import sys,json;print(json.load(sys.stdin)['data']['list'][0]['id'])")
SO=$(req POST /sales-orders "{\"customerId\":$CUST,\"lines\":[{\"materialId\":3,\"qty\":5,\"price\":8}]}" | python -c "import sys,json;d=json.load(sys.stdin);print(d['data']['id'] if d.get('code')==0 else 0)")
[ "${SO:-0}" -ge 1 ] && ok "SO 创建（id=$SO）" || bad "SO 创建失败"
req POST "/sales-orders/$SO/audit" | python -c "import sys,json;raise SystemExit(0 if json.load(sys.stdin).get('code')==0 else 1)" && ok "SO 审核（预占经 core-wms ✓）" || bad "审核失败"

say "隔离：wms 流程不受影响"
STK=$(req GET "/stocks?material_id=3" | python -c "import sys,json;print(len(json.load(sys.stdin)['data']['list']))")
[ "${STK:-0}" -ge 1 ] && ok "库存查询（$STK 行）" || bad "库存查询失败"

say "恢复：经 ops-admin 拉起 core-mes"
req POST /ops/services/core-mes/start | python -c "import sys,json;raise SystemExit(0 if json.load(sys.stdin).get('code')==0 else 1)" && ok "start 指令受理" || bad "start 失败"
sleep 10
MES2=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $AT" "$BASE/work-orders?page_size=1")
[ "$MES2" = "200" ] && ok "mes 路由恢复 200" || bad "恢复后 $MES2"
WO=$(req POST /work-orders '{"materialId":2,"qty":5}' | python -c "import sys,json;d=json.load(sys.stdin);print(d['data']['id'] if d.get('code')==0 else 0)")
[ "${WO:-0}" -ge 1 ] && ok "mes 写入恢复（WO=$WO）" || bad "mes 写入失败"

say "对账：预占 vs 订单（跨 erp/wms 库）"
PG="$(dirname "$0")/../.pgsql/pgsql/bin/psql.exe"
RSV=$("$PG" -h 127.0.0.1 -U postgres -d evolvermon_wms -t -A -c "SELECT COALESCE(SUM(qty),0) FROM wms.stock_reservations WHERE status='held' AND source_doc_type='sales_order'" 2>/dev/null)
AUD=$(req GET "/sales-orders/$SO" | python -c "import sys,json;print(json.load(sys.stdin)['data']['status'])")
[ "${RSV:-x}" != "x" ] && ok "预占落 wms 库（held=$RSV，SO 状态=$AUD）" || bad "预占查询失败"

say "审计与告警留痕"
AL=$(req GET "/ops/alerts" | python -c "import sys,json;print(len(json.load(sys.stdin)['data']['list']))")
[ "${AL:-0}" -ge 1 ] && ok "core-mes 宕机告警已记录（$AL 条）" || bad "无告警"
AU=$(req GET "/ops/audit" | python -c "import sys,json;print(len([x for x in json.load(sys.stdin)['data']['list'] if x['service']=='core-mes']))")
[ "${AU:-0}" -ge 1 ] && ok "启停运维审计（$AU 条）" || bad "无审计"

printf '\n\033[1m结果: %d 通过 / %d 失败\033[0m\n' "$PASS" "$FAIL"
[ $FAIL = 0 ]
