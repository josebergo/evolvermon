#!/usr/bin/env bash
# 事件一致性 + 主数据读通道验证（Phase 2，前置：run-micro.sh 拓扑就绪）。
# 断言：① 业务事件落 outbox 且 relay 全部投递（sent_at 非空、dead=0）
#      ② 消费幂等表去重生效
#      ③ master-data 内部 API 批量读 + 经网关 MRP（走读通道）正常
set -uo pipefail
export PYTHONUTF8=1 PYTHONIOENCODING=utf-8
BASE="${1:-http://127.0.0.1:8080}/api/v1"
PGSQL="$(dirname "$0")/../.pgsql/pgsql/bin/psql.exe"
PASS=0; FAIL=0
say()  { printf '\n\033[36m== %s ==\033[0m\n' "$*"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$*"; PASS=$((PASS+1)); }
bad()  { printf '  \033[31m✗\033[0m %s\n' "$*"; FAIL=$((FAIL+1)); }

AT=$(curl -s -X POST -H "Content-Type: application/json" -d '{"username":"admin","password":"Admin@123"}' "$BASE/auth/login" | python -c "import sys,json;print(json.load(sys.stdin)['data']['accessToken'])")
[ -n "$AT" ] && ok "登录" || { bad "登录失败"; exit 1; }

say "outbox：事件落箱 + relay 投递"
# 触发一批业务事件（建单/审核产生 domain events）
MAT=$(curl -s -H "Authorization: Bearer $AT" "$BASE/materials?category=semi&page_size=1" | python -c "import sys,json;print(json.load(sys.stdin)['data']['list'][0]['id'])")
CUST=$(curl -s -H "Authorization: Bearer $AT" "$BASE/customers?page_size=1" | python -c "import sys,json;print(json.load(sys.stdin)['data']['list'][0]['id'])")
SO=$(curl -s -X POST -H "Content-Type: application/json" -H "Authorization: Bearer $AT" \
  -d "{\"customerId\":$CUST,\"lines\":[{\"materialId\":$MAT,\"qty\":8,\"price\":100}]}" \
  "$BASE/sales-orders" | python -c "import sys,json;print(json.load(sys.stdin)['data']['id'])")
curl -s -X POST -H "Authorization: Bearer $AT" "$BASE/sales-orders/$SO/audit" >/dev/null
sleep 8  # 等 relay 一轮
# 拓扑无关：lite/mono 时 outbox 在 evolvermon 库；split 时散在 master/erp/mes/wms 四库——
# 跨全部候选库求和，无该表的库计 0（2>/dev/null 吞掉 does-not-exist）。
ALL_DBS="evolvermon evolvermon_identity evolvermon_master evolvermon_erp evolvermon_mes evolvermon_wms evolvermon_integration evolvermon_ops"
qsum() { # qsum <sql> —— 跨库求和
  local t=0 v d
  for d in $ALL_DBS; do
    v=$("$PGSQL" -h 127.0.0.1 -U postgres -d "$d" -t -A -c "$1" 2>/dev/null | head -1)
    [ -n "$v" ] && t=$((t+v))
  done
  echo "$t"
}
TOTAL=$(qsum "SELECT count(*) FROM platform.outbox_messages")
SENT=$(qsum "SELECT count(*) FROM platform.outbox_messages WHERE sent_at IS NOT NULL")
DEAD=$(qsum "SELECT count(*) FROM platform.outbox_messages WHERE attempts>=5 AND sent_at IS NULL")
[ "${TOTAL:-0}" -ge 1 ] && ok "事件落箱 $TOTAL 条" || bad "outbox 空"
[ "$SENT" = "$TOTAL" ] && ok "relay 全部投递（$SENT/$TOTAL）" || bad "未投递 $((TOTAL-SENT)) 条"
[ "${DEAD:-0}" = "0" ] && ok "无放弃投递（dead=0）" || bad "放弃 $DEAD 条"

say "消费幂等"
DUP=$(qsum "SELECT count(*) FROM (SELECT consumer,message_id FROM platform.consumed_messages GROUP BY 1,2 HAVING count(*)>1) t")
[ "${DUP:-0}" = "0" ] && ok "幂等表无重复（唯一约束生效）" || bad "存在 $DUP 组重复"

say "master-data 内部 API + MRP 读通道"
TOK="${EM_INTERNAL_TOKEN:-dev-internal-token}"
EDGES=$(curl -s -H "X-Internal-Token: $TOK" "http://127.0.0.1:8085/internal/master/bom-edges?tenantId=1" | python -c "import sys,json;print(len(json.load(sys.stdin)['data']['list']))")
[ "${EDGES:-0}" -ge 1 ] && ok "BOM 边批量拉取：$EDGES 条" || bad "bom-edges 空/失败: $EDGES"
BADTOK=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:8085/internal/master/bom-edges?tenantId=1")
[ "$BADTOK" = "401" ] && ok "内部令牌校验：无令牌被拒" || bad "内部 API 未鉴权（$BADTOK）"
MATL=$(curl -s -H "X-Internal-Token: $TOK" "http://127.0.0.1:8085/internal/master/materials?tenantId=1&ids=$MAT" | python -c "import sys,json;print(len(json.load(sys.stdin)['data']['list']))")
[ "${MATL:-0}" = "1" ] && ok "物料批量读（ids 过滤）" || bad "物料批量读失败: $MATL"
MRP=$(curl -s -X POST -H "Content-Type: application/json" -H "Authorization: Bearer $AT" -d '{"ignoreSafety":true}' "$BASE/mrp/run")
echo "$MRP" | python -c "import sys,json;d=json.load(sys.stdin);raise SystemExit(0 if d.get('code')==0 else 1)" && ok "MRP 经主数据读通道运行正常（core EM_MASTER_URL→master-data）" || bad "MRP 失败: $(echo "$MRP" | head -c 120)"

printf '\n\033[1m结果: %d 通过 / %d 失败\033[0m\n' "$PASS" "$FAIL"
[ $FAIL = 0 ]
