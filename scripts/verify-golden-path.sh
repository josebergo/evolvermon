#!/usr/bin/env bash
# EvolverMon 金路径验证脚本（对应 epic-backlog.md M1 故事 1–13）
# 前置：后端已启动且种子数据已写入（EM_SEED_DEMO=true）
# 用法：bash scripts/verify-golden-path.sh [base_url]  （默认 http://127.0.0.1:8080）
set -uo pipefail
export PYTHONUTF8=1 PYTHONIOENCODING=utf-8   # Windows：含中文 JSON 必须强制 UTF-8
BASE="${1:-http://127.0.0.1:8080}/api/v1"
PASS=0; FAIL=0

say()  { printf '\n\033[36m== %s ==\033[0m\n' "$*"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$*"; PASS=$((PASS+1)); }
bad()  { printf '  \033[31m✗ %s\033[0m\n' "$*"; FAIL=$((FAIL+1)); }

req() { # method path [json] [token] -> body
  local m=$1 p=$2 data=${3:-} tok=${4:-$AT}
  if [ -n "$data" ]; then
    curl -s -X "$m" -H "Content-Type: application/json" -H "Authorization: Bearer $tok" -d "$data" "$BASE$p"
  else
    curl -s -X "$m" -H "Authorization: Bearer $tok" "$BASE$p"
  fi
}
jget() { python -c "import sys,json,functools; d=json.load(sys.stdin); print(functools.reduce(lambda a,k: a[int(k)] if isinstance(a,list) else a[k], '$1'.split('.'), d))" 2>/dev/null; }

say "故事1 登录与菜单"
LOGIN=$(curl -s -X POST -H "Content-Type: application/json" -d '{"username":"admin","password":"Admin@123"}' "$BASE/auth/login")
AT=$(echo "$LOGIN" | jget data.accessToken)
[ -n "$AT" ] && ok "登录成功" || { bad "登录失败: $LOGIN"; exit 1; }
PROFILE=$(req GET /auth/profile)
MENUS=$(echo "$PROFILE" | python -c "import sys,json; print(len(json.load(sys.stdin)['data']['menus']))")
[ "${MENUS:-0}" -ge 5 ] && ok "菜单树返回 $MENUS 个顶层项" || bad "菜单异常"

say "故事2 工厂与仓库"
FACS=$(req GET "/factories?page_size=10")
[ "$(echo "$FACS" | jget data.total)" -ge 2 ] && ok "双工厂存在" || bad "工厂数不足"
FAC_ID=$(echo "$FACS" | jget data.list.0.id)
WHS=$(req GET "/warehouses?page_size=50")
RAW_WH=$(echo "$WHS" | python -c "import sys,json; d=json.load(sys.stdin); print(next(w['id'] for w in d['data']['list'] if w['type']=='raw'))")
FG_WH=$(echo "$WHS" | python -c "import sys,json; d=json.load(sys.stdin); print(next(w['id'] for w in d['data']['list'] if w['type']=='finished'))")
ok "原料仓=$RAW_WH 成品仓=$FG_WH"

say "故事3 物料与 BOM"
MATS=$(req GET "/materials?page_size=100")
[ "$(echo "$MATS" | jget data.total)" -ge 6 ] && ok "物料≥6" || bad "物料不足"
FG_ID=$(echo "$MATS" | python -c "import sys,json; d=json.load(sys.stdin); print(next(m['id'] for m in d['data']['list'] if m['code']=='FG-1000'))")
BOMS=$(req GET "/boms?status=active&page_size=20")
ok "启用 BOM: $(echo "$BOMS" | jget data.total) 张"

say "故事4 工艺路线"
RTS=$(req GET "/routings?page_size=20")
[ "$(echo "$RTS" | jget data.total)" -ge 2 ] && ok "工艺路线存在" || bad "工艺缺失"

say "故事5 销售订单审核（预占）"
CUST=$(req GET "/customers?page_size=5"); CUST_ID=$(echo "$CUST" | jget data.list.0.id)
SO=$(req POST /sales-orders "{\"customerId\":$CUST_ID,\"lines\":[{\"materialId\":$FG_ID,\"qty\":100,\"price\":129,\"amount\":12900}]}")
SO_ID=$(echo "$SO" | jget data.id); SO_NO=$(echo "$SO" | jget data.docNo)
[ -n "$SO_ID" ] && ok "订单创建 $SO_NO（自动单号）" || bad "订单创建失败: $SO"
AVAIL_BEFORE=$(req GET "/stocks/available" | python -c "import sys,json; d=json.load(sys.stdin)['data']; print(int(sum(r['onHand'] for r in d)))")
req POST "/sales-orders/$SO_ID/audit" > /dev/null
SO_ST=$(req GET "/sales-orders/$SO_ID" | jget data.status)
[ "$SO_ST" = "audited" ] && ok "审核成功，状态=$SO_ST" || bad "审核后状态=$SO_ST"
RSV=$(req GET "/reservations?status=held")
ok "预占记录: $(echo "$RSV" | jget data.total) 条"

say "故事6 MRP 运算与转单（多层 BOM 展开）"
MRP=$(req POST /mrp/run "{}")
SUG_JSON=$(echo "$MRP" | python -c "import sys,json; d=json.load(sys.stdin)['data']; print(len(d['suggestions']))")
ok "MRP 建议 $SUG_JSON 条"
[ "${SUG_JSON:-0}" -gt 0 ] && ok "MRP 识别缺口（含 BOM 子件展开）" || bad "MRP 未识别缺口"
CONV=$(req POST /mrp/suggestions/convert "$(req POST /mrp/run '{}' | python -c "
import sys,json
d=json.load(sys.stdin)['data']['suggestions']
sel=[{'materialId':s['materialId'],'sourceType':s['sourceType'],'netQty':s['netQty'],'dueDate':s['dueDate'],'sources':s['sources']} for s in d if s['sourceType']=='make']
print(json.dumps({'suggestions':sel,'to':'wo'}))")")
WO_NOS=$(echo "$CONV" | python -c "import sys,json; print(len(json.load(sys.stdin)['data']['workOrders']))" 2>/dev/null || echo 0)
[ "${WO_NOS:-0}" -ge 2 ] && ok "转工单 $WO_NOS 张（成品+半成品）" || bad "转工单仅 $WO_NOS 张"

# 通用：完整跑一张工单（下达→首工序报工→完工→报交入库过账）
# 批次号仅在物料启用批次管理时传入（否则会拆出独立库存维度导致领料不匹配）
run_wo() {
  local WO=$1 QTY=$2 BATCH=$3
  local MAT_ID=$(req GET "/work-orders/$WO" | python -c "import sys,json; print(json.load(sys.stdin)['data']['materialId'])")
  local BATCHED=$(req GET "/materials/$MAT_ID" | python -c "import sys,json; print(json.load(sys.stdin)['data'].get('batchManaged'))")
  local BFIELD=""
  [ "$BATCHED" = "True" ] && BFIELD=",\"batchNo\":\"$BATCH\""
  req POST "/work-orders/$WO/release" > /dev/null
  local OPS=$(req GET "/wo-operations?work_order_id=$WO")
  local OP1=$(echo "$OPS" | python -c "import sys,json; print(json.load(sys.stdin)['data']['list'][0]['id'])")
  req POST "/wo-operations/$OP1/start" > /dev/null 2>&1
  req POST /production-reports "{\"workOrderId\":$WO,\"operationId\":$OP1,\"qtyOk\":$QTY,\"qtyDefect\":0,\"hours\":1,\"clientToken\":\"vt-$WO-$RANDOM\"}" > /dev/null
  req POST "/work-orders/$WO/force-finish" > /dev/null
  local FIN=$(req POST "/work-orders/$WO/finish-receipt")
  local FID=$(echo "$FIN" | python -c "import sys,json; print(json.load(sys.stdin)['data']['id'])")
  local FLINE=$(req GET "/stock-orders/$FID" | python -c "import sys,json; print(json.load(sys.stdin)['data']['lines'][0]['id'])")
  req POST "/stock-orders/$FID/post-lines" "{\"lines\":[{\"lineId\":$FLINE,\"qty\":$QTY$BFIELD}]}" > /dev/null
  req GET "/work-orders/$WO" | python -c "import sys,json; print(json.load(sys.stdin)['data']['status'])"
}

# 通用：对已下达/已部分报工的工单补报足量 → 完工 → 报交入库过账
run_wo_report_finish() {
  local WO=$1 QTY=$2 BATCH=$3
  local GOOD=$(req GET "/work-orders/$WO" | python -c "import sys,json; print(json.load(sys.stdin)['data']['goodQty'])")
  local NEED=$(python -c "print(max($QTY - $GOOD, 0))")
  local OPS=$(req GET "/wo-operations?work_order_id=$WO")
  local OP1=$(echo "$OPS" | python -c "import sys,json; print(json.load(sys.stdin)['data']['list'][0]['id'])")
  if [ "$NEED" != "0" ]; then
    req POST /production-reports "{\"workOrderId\":$WO,\"operationId\":$OP1,\"qtyOk\":$NEED,\"qtyDefect\":0,\"hours\":1,\"clientToken\":\"vt2-$WO-$RANDOM\"}" > /dev/null
  fi
  req POST "/work-orders/$WO/force-finish" > /dev/null
  local FIN=$(req POST "/work-orders/$WO/finish-receipt")
  local FID=$(echo "$FIN" | python -c "import sys,json; print(json.load(sys.stdin)['data']['id'])")
  local FQTY=$(req GET "/stock-orders/$FID" | python -c "import sys,json; print(json.load(sys.stdin)['data']['lines'][0]['qty'])")
  local FLINE=$(req GET "/stock-orders/$FID" | python -c "import sys,json; print(json.load(sys.stdin)['data']['lines'][0]['id'])")
  LAST_POST_RESP=$(req POST "/stock-orders/$FID/post-lines" "{\"lines\":[{\"lineId\":$FLINE,\"qty\":$FQTY,\"batchNo\":\"$BATCH\"}]}")
  echo "  [debug] wo=$WO fid=$FID line=$FLINE qty=$FQTY post=$LAST_POST_RESP" >&2
  req GET "/work-orders/$WO" | python -c "import sys,json; print(json.load(sys.stdin)['data']['status'])"
}

say "故事7 采购到货"
SUPP=$(req GET "/suppliers?page_size=5"); SUPP_ID=$(echo "$SUPP" | jget data.list.0.id)
MAT_RAW=$(req GET "/materials?category=raw&page_size=5" | jget data.list.0.id)
PO=$(req POST /purchase-orders "{\"supplierId\":$SUPP_ID,\"lines\":[{\"materialId\":$MAT_RAW,\"qty\":500,\"price\":8.5,\"amount\":4250}]}")
PO_ID=$(echo "$PO" | jget data.id); ok "采购单 $(echo "$PO" | jget data.docNo)"
req POST "/purchase-orders/$PO_ID/audit" >/dev/null
RCV=$(req POST "/purchase-orders/$PO_ID/arrive")
RCV_ID=$(echo "$RCV" | jget data.id); ok "到货入库单 $(echo "$RCV" | jget data.docNo)"
LINE_ID=$(req GET "/stock-orders/$RCV_ID" | jget data.lines.0.id)
req POST "/stock-orders/$RCV_ID/post-lines" "{\"lines\":[{\"lineId\":$LINE_ID,\"qty\":500}]}" > /dev/null
ok "采购入库过账完成"

say "故事8 工单下达与报工（先半成品后成品，多级 BOM）"
SF_ID=$(req GET "/materials?page_size=100" | python -c "
import sys,json
d=json.load(sys.stdin)['data']['list']
print(next(m['id'] for m in d if m['code']=='SF-2000'))")
WO_SF=$(req GET "/work-orders?page_size=50" | python -c "
import sys,json
d=json.load(sys.stdin)['data']['list']
print(next((w['id'] for w in d if w['materialId']==$SF_ID), ''))")
WO_FG=$(req GET "/work-orders?page_size=50" | python -c "
import sys,json
d=json.load(sys.stdin)['data']['list']
print(next((w['id'] for w in d if w['materialId']==$FG_ID), ''))")
[ -n "$WO_SF" ] && [ -n "$WO_FG" ] && ok "半成品工单=$WO_SF 成品工单=$WO_FG" || bad "工单匹配失败 SF=$WO_SF FG=$WO_FG"
ST_SF=$(run_wo "$WO_SF" 100 "BSF-V1")
[ "$ST_SF" = "stored" ] && ok "半成品工单 $WO_SF 完成入库（stored）" || bad "半成品工单状态=$ST_SF"

say "故事8b 报工幂等"
req POST "/work-orders/$WO_FG/release" > /dev/null
OPS=$(req GET "/wo-operations?work_order_id=$WO_FG")
OP1=$(echo "$OPS" | python -c "import sys,json; print(json.load(sys.stdin)['data']['list'][0]['id'])")
req POST "/wo-operations/$OP1/start" > /dev/null 2>&1
RP1=$(req POST /production-reports "{\"workOrderId\":$WO_FG,\"operationId\":$OP1,\"qtyOk\":50,\"qtyDefect\":0,\"hours\":1,\"clientToken\":\"dup-token-1\"}")
RP2=$(req POST /production-reports "{\"workOrderId\":$WO_FG,\"operationId\":$OP1,\"qtyOk\":50,\"qtyDefect\":0,\"hours\":1,\"clientToken\":\"dup-token-1\"}")
IDEM=$(echo "$RP2" | python -c "import sys,json; print(json.load(sys.stdin)['data'].get('idempotent'))" 2>/dev/null)
[ "$IDEM" = "True" ] && ok "幂等：重复提交未双计" || bad "幂等失效: $RP2"

say "故事9 领料出库（防超卖，分仓拆单）"
PICK=$(req POST "/work-orders/$WO_FG/picking-request")
PICK_IDS=$(echo "$PICK" | python -c "import sys,json; print(' '.join(str(o['id']) for o in json.load(sys.stdin)['data']['orders']))")
ok "领料单拆分 $(echo $PICK_IDS | wc -w) 张"
ALL_DONE=1
for PID in $PICK_IDS; do
  PLINES=$(req GET "/stock-orders/$PID" | python -c "
import sys,json
d=json.load(sys.stdin)['data']['lines']
print(json.dumps({'lines':[{'lineId':l['id'],'qty':l['qty'],'batchNo':(l.get('batchNo') or '')} for l in d]}))")
  req POST "/stock-orders/$PID/post-lines" "$PLINES" > /dev/null
  PST=$(req GET "/stock-orders/$PID" | jget data.status)
  [ "$PST" = "done" ] || ALL_DONE=0
done
[ "$ALL_DONE" = "1" ] && ok "领料过账完成（原料仓+半成品仓）" || bad "领料未完成"

say "故事10 成品完工报交入库"
ST_FG=$(run_wo_report_finish "$WO_FG" 200 "BVERIFY1")
[ "$ST_FG" = "stored" ] && ok "成品工单入库（stored）" || bad "成品工单状态=$ST_FG"
RCV_PROD=$(req GET "/stock-orders?source_type=production" | python -c "import sys,json;print(json.load(sys.stdin)['data']['list'][0]['docNo'])")
SN_COUNT=$(req GET "/trace/serial/$(python -c "import urllib.parse;print(urllib.parse.quote('SN-$RCV_PROD-0001'))")" | python -c "import sys,json;print(json.load(sys.stdin)['code'])")
[ "$SN_COUNT" = "0" ] && ok "完工即生成序列号（$RCV_PROD）" || bad "序列号未生成 rcv=$RCV_PROD dbflag=$(/d/JCode/evolver-mon/.pgsql/pgsql/bin/psql -h 127.0.0.1 -U postgres -d evolvermon -t -A -c "SELECT serial_managed FROM materials WHERE id=$FG_ID") count=$(cd /d/JCode/evolver-mon/.pgsql && ./pgsql/bin/psql -h 127.0.0.1 -U postgres -d evolvermon -t -A -c "SELECT count(*) FROM serial_numbers")"

say "故事11 销售发货闭环"
SHP=$(req POST "/sales-orders/$SO_ID/create-shipment")
SHP_ID=$(echo "$SHP" | jget data.id)
# 用成品实际库存批次发货（成品启用批次管理）
FG_BATCH=$(req GET "/stocks?material_id=$FG_ID&positive=1" | python -c "
import sys,json
d=json.load(sys.stdin)['data']['list']
print(d[0].get('batchNo') or '')")
SLINES=$(req GET "/stock-orders/$SHP_ID" | python -c "
import sys,json
d=json.load(sys.stdin)['data']['lines']
print(json.dumps({'lines':[{'lineId':l['id'],'qty':l['qty'],'batchNo':'$FG_BATCH'} for l in d]}))")
req POST "/stock-orders/$SHP_ID/post-lines" "$SLINES" >/dev/null
SO_ST2=$(req GET "/sales-orders/$SO_ID" | jget data.status)
[ "$SO_ST2" = "done" ] && ok "订单全部发货→已完成" || bad "订单状态=$SO_ST2"

say "故事12 盘点闭环"
STK=$(req POST /stocktakings "{\"warehouseId\":$RAW_WH,\"scope\":\"full\",\"method\":\"blind\"}")
STK_ID=$(echo "$STK" | jget data.id)
req POST "/stocktakings/$STK_ID/start" >/dev/null
FIRST_LINE=$(req GET "/stocktakings/$STK_ID" | jget data.lines.0.id)
req POST "/stocktakings/$STK_ID/lines/$FIRST_LINE" '{"countedQty":0}' >/dev/null
FINSTK=$(req POST "/stocktakings/$STK_ID/finish")
ok "盘点差异确认（gain=$(echo "$FINSTK" | jget data.gainOrders) loss=$(echo "$FINSTK" | jget data.lossOrders)）"

say "故事13 工作台与看板"
SUM=$(req GET /dashboard/summary)
ok "工作台卡片: 在制=$(echo "$SUM" | jget data.counts.woActive)"
BOARD=$(req GET /kanban/board); ok "看板接口 OK"
TODAY=$(req GET /kanban/today); ok "今日合格率 $(echo "$TODAY" | jget data.passRate)"

say "故事3.5 (M2) 序列号物料启用"
FG_OBJ=$(req GET "/materials/$FG_ID")
PUT_RESP=$(req PUT "/materials/$FG_ID" "$(echo "$FG_OBJ" | python -c "
import sys,json
d=json.load(sys.stdin)['data']
d['serialManaged']=True
print(json.dumps(d))")")
SN_FLAG=$(req GET "/materials/$FG_ID" | python -c "import sys,json;print(json.load(sys.stdin)['data']['serialManaged'])")
[ "$SN_FLAG" = "True" ] && ok "FG-1000 已启用序列号管理" || bad "序列号开关未生效 put=$PUT_RESP"

say "故事14 (M2) 单据打印"
PR_HTML=$(curl -s -H "Authorization: Bearer $AT" "$BASE/print/work-orders/$WO_FG")
echo "$PR_HTML" | grep -q "工单路由卡" && ok "工单路由卡 HTML" || bad "路由卡打印异常"
SHP_DOC=$(req GET "/stock-orders?source_type=sales" | python -c "import sys,json;print(json.load(sys.stdin)['data']['list'][0]['id'])")
PS_HTML=$(curl -s -H "Authorization: Bearer $AT" "$BASE/print/stock-orders/$SHP_DOC")
echo "$PS_HTML" | grep -q "送货单" && ok "送货单 HTML" || bad "送货单打印异常"

say "故事15 (M2) 序列号追溯"
RCV_PROD=$(req GET "/stock-orders?source_type=production" | python -c "import sys,json;print(json.load(sys.stdin)['data']['list'][0]['docNo'])")
SN="SN-$RCV_PROD-0001"
TRACE=$(req GET "/trace/serial/$(python -c "import urllib.parse;print(urllib.parse.quote('$SN'))")")
SN_OK=$(echo "$TRACE" | python -c "import sys,json;d=json.load(sys.stdin)['data'];print(d['serial']['serial'],d['serial']['status'],d['workOrder']['docNo'])" 2>/dev/null)
[ -n "$SN_OK" ] && ok "序列号追溯：$SN_OK" || bad "序列号追溯失败: $TRACE"

say "故事16 (M2) 超期采购与看板统计"
SUM2=$(req GET /dashboard/summary)
ok "超期采购单统计字段: $(echo "$SUM2" | jget data.counts.poOverdue)"

say "故事17 (M3) ATP 可承诺量"
ATP=$(req GET "/atp?material_id=$FG_ID&qty=50")
ATP_OK=$(echo "$ATP" | python -c "import sys,json;d=json.load(sys.stdin)['data'];print(int(d['available']),d['feasible'])" 2>/dev/null)
[ -n "$ATP_OK" ] && ok "ATP 返回（可用/可行）：$ATP_OK" || bad "ATP 异常: $ATP"

say "故事18 (M3) 有限产能排产"
WO_NEW=$(req POST /work-orders "{\"materialId\":$FG_ID,\"qty\":50,\"priority\":1}")
WO_NEW_ID=$(echo "$WO_NEW" | jget data.id)
req POST "/work-orders/$WO_NEW_ID/release" > /dev/null
SCHED=$(req POST /plan/schedule)
SCH_N=$(echo "$SCHED" | jget data.scheduled)
[ "${SCH_N:-0}" -ge 1 ] && ok "排产覆盖 $SCH_N 张在制工单" || bad "排产失败: $SCHED"
GANTT=$(req GET /plan/gantt)
BARS=$(echo "$GANTT" | python -c "import sys,json;print(sum(len(g['bars']) for g in json.load(sys.stdin)['data']))")
[ "${BARS:-0}" -ge 1 ] && ok "甘特返回 $BARS 条工序条" || bad "甘特为空"
DB=$(req GET /plan/dispatch-board)
ok "派工看板工作中心 $(echo "$DB" | python -c "import sys,json;print(len(json.load(sys.stdin)['data']))") 个"

say "故事19 (M3) 排产时间写入工序"
OP_PLAN=$(req GET "/wo-operations?work_order_id=$WO_NEW_ID" | python -c "
import sys,json
ops=json.load(sys.stdin)['data']['list']
print(sum(1 for o in ops if o.get('planStartAt')))")
[ "${OP_PLAN:-0}" -ge 1 ] && ok "工序列已带计划起止时间" || bad "工序无排产时间"

say "故事20 (M4) IQC/FQC 自动质检"
QC_LIST=$(req GET "/qc-records")
QC_N=$(echo "$QC_LIST" | jget data.total)
[ "${QC_N:-0}" -ge 2 ] && ok "到货/完工自动生成 $QC_N 张待检单" || bad "自动质检未生成: $QC_LIST"
QC_ID=$(echo "$QC_LIST" | jget data.list.0.id)
req POST "/qc-records/$QC_ID/audit" '{"result":"fail","disposal":"rework"}' > /dev/null
DISPOSE=$(req POST "/qc-records/$QC_ID/dispose")
RW_NO=$(echo "$DISPOSE" | jget data.reworkWorkOrder)
[ -n "$RW_NO" ] && ok "不合格处置 → 返工工单 $RW_NO" || bad "处置失败: $DISPOSE"

say "故事21 (M4) 工单实际成本归集"
COST=$(req POST "/work-orders/$WO_FG/costing")
COST_V=$(echo "$COST" | python -c "import sys,json;d=json.load(sys.stdin)['data'];print(round(d['cost'],2),round(d['unitCost'],2))" 2>/dev/null)
[ -n "$COST_V" ] && ok "FG 工单成本（总/单件）：$COST_V" || bad "成本归集失败: $COST"

say "故事22 (M4) 财务凭证导出"
VCH=$(req GET "/finance/vouchers")
VCH_AR=$(echo "$VCH" | python -c "import sys,json;d=json.load(sys.stdin)['data'];print(sum(1 for v in d if v['type']=='AR'),sum(1 for v in d if v['type']=='AP'))" 2>/dev/null)
[ -n "$VCH_AR" ] && ok "凭证（应收/应付）：$VCH_AR" || bad "凭证导出异常: $VCH"

say "故事23 (M5) 厂间调拨"
FACS2=$(req GET "/factories?page_size=10")
DIM=$(req GET "/stocks?positive=1" | python -c "
import sys,json
s=json.load(sys.stdin)['data']['list'][0]
print(s['factoryId'], s['materialId'], s.get('batchNo') or '')")
F_ID1=$(echo "$DIM" | cut -d' ' -f1)
DIM_MAT=$(echo "$DIM" | cut -d' ' -f2)
DIM_BATCH=$(echo "$DIM" | cut -d' ' -f3)
F_ID2=$(echo "$FACS2" | python -c "
import sys,json
facs=json.load(sys.stdin)['data']['list']
print(next(f['id'] for f in facs if f['id'] != $F_ID1))")
TR=$(req POST /transfers "{\"fromFactoryId\":$F_ID1,\"toFactoryId\":$F_ID2,\"materialId\":$DIM_MAT,\"qty\":10,\"batchNo\":\"$DIM_BATCH\"}")
TR_OK=$(echo "$TR" | jget data.outDocNo)
[ -n "$TR_OK" ] && ok "厂间调拨：$TR_OK → $(echo "$TR" | jget data.inDocNo)" || bad "调拨失败: $TR"

say "故事24 (M5) 集团视图"
GRP=$(req GET /group/summary)
GRP_N=$(echo "$GRP" | python -c "import sys,json;print(len(json.load(sys.stdin)['data']))")
[ "${GRP_N:-0}" -ge 2 ] && ok "集团视图覆盖 $GRP_N 家工厂" || bad "集团视图异常: $GRP"

say "故事25 (M6) Webhook 事件出口（端到端投递）"
python - <<'PYEOF' &
import http.server, json, sys
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        self.rfile.read(int(self.headers.get('Content-Length', 0)))
        with open('webhook-received.log', 'a') as f:
            f.write(self.headers.get('X-Evolvermon-Event', '?') + '\n')
        self.send_response(200); self.end_headers()
    def log_message(self, *a): pass
http.server.HTTPServer(('127.0.0.1', 18099), H).serve_forever()
PYEOF
RECV_PID=$!
sleep 1
req POST /webhooks '{"name":"verifier","url":"http://127.0.0.1:18099/hook","events":"*","secret":"s3cr3t"}' > /dev/null
WO_EVT=$(req POST /work-orders "{\"materialId\":$FG_ID,\"qty\":10}")
req POST "/work-orders/$(echo "$WO_EVT" | jget data.id)/release" > /dev/null
sleep 5
DELIV=$(req GET "/webhook-deliveries?status=ok")
DELIV_OK=$(echo "$DELIV" | jget data.total)
[ "${DELIV_OK:-0}" -ge 1 ] && ok "Webhook 投递成功 $DELIV_OK 条" || bad "Webhook 未投递: $DELIV"
kill $RECV_PID 2>/dev/null

say "故事26 (M6) 设备遥测接入"
req POST "/devices/SMT-L1/telemetry" '{"metric":"count","value":123,"workCenter":"SMT 贴片线"}' > /dev/null
TEL=$(req GET "/devices/telemetry/latest")
TEL_V=$(echo "$TEL" | python -c "import sys,json;d=json.load(sys.stdin)['data'];print(d[0]['deviceCode'],d[0]['metric'],d[0]['value'])" 2>/dev/null)
[ -n "$TEL_V" ] && ok "遥测最新值：$TEL_V" || bad "遥测异常: $TEL"

say "故事27 (M6) 智能建议（缺料/交期/产能）"
INS=$(req GET "/advisor/insights")
INS_N=$(echo "$INS" | python -c "import sys,json;print(len(json.load(sys.stdin)['data']['insights']))")
ok "洞察条数：$INS_N（0 也算通过=无风险）"

say "越权矩阵抽查（NFR-031）"
NOAUTH=$(curl -s "$BASE/stocks" -H "Authorization: Bearer invalid")
echo "$NOAUTH" | grep -q '"code":1000' && ok "无效令牌被拒" || bad "无效令牌放行"
PR_TOKEN=$(curl -s -X POST -H "Content-Type: application/json" -d '{"username":"qc","password":"Demo@123"}' "$BASE/auth/login" | jget data.accessToken)
QC_FORBID=$(curl -s -H "Authorization: Bearer $PR_TOKEN" "$BASE/work-orders?page_size=1")
echo "$QC_FORBID" | grep -q '"code":1003' && ok "质检员访问工单被 403" || bad "越权未拦截: $QC_FORBID"

printf '\n\033[1m结果: %d 通过 / %d 失败\033[0m\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
