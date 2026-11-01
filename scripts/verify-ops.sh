#!/usr/bin/env bash
# 统一管理平台验证（Phase 1.5，前置：run-micro.sh 拓扑已就绪）。
# 断言：① 服务注册与健康聚合 ② 菜单含系统运维 ③ 启停控制 + 审计 + 告警生命周期 ④ 故障隔离（停 integration 核心流转不受影响）
set -uo pipefail
export PYTHONUTF8=1 PYTHONIOENCODING=utf-8
BASE="${1:-http://127.0.0.1:8080}/api/v1"
PASS=0; FAIL=0
say()  { printf '\n\033[36m== %s ==\033[0m\n' "$*"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$*"; PASS=$((PASS+1)); }
bad()  { printf '  \033[31m✗\033[0m %s\n' "$*"; FAIL=$((FAIL+1)); }
req()  { local m=$1 p=$2 d=${3:-}; if [ -n "$d" ]; then curl -s -X "$m" -H "Content-Type: application/json" -H "Authorization: Bearer $AT" -d "$d" "$BASE$p"; else curl -s -X "$m" -H "Authorization: Bearer $AT" "$BASE$p"; fi; }

AT=$(curl -s -X POST -H "Content-Type: application/json" -d '{"username":"admin","password":"Admin@123"}' "$BASE/auth/login" | python -c "import sys,json;print(json.load(sys.stdin)['data']['accessToken'])")
[ -n "$AT" ] && ok "登录（经网关→identity）" || { bad "登录失败"; exit 1; }

# 等待健康轮询收敛（10s 周期；等待全部受管服务 up，最多 45s）
for i in $(seq 1 45); do
  downs=$(curl -s -H "Authorization: Bearer $AT" "$BASE/ops/services" | python -c "import sys,json;d=json.load(sys.stdin)['data']['list'];print(','.join(x['name'] for x in d if x['name']!='gateway' and x['status']!='up'))" 2>/dev/null)
  [ -z "$downs" ] && break
  sleep 1
done

say "服务注册与健康聚合"
SVCS=$(req GET /ops/services)
for s in $(echo "$SVCS" | python -c "import sys,json;print(' '.join(x['name'] for x in json.load(sys.stdin)['data']['list'] if x['name']!='gateway'))"); do
  echo "$SVCS" | python -c "
import sys,json
d=json.load(sys.stdin)['data']['list']
m={x['name']:x for x in d}
want='$s'
if want not in m: print('missing'); raise SystemExit(1)
st=m[want]['status']
print(st)" | grep -q "^up$" && ok "$s UP" || bad "$s 非 UP：$(echo "$SVCS" | python -c "import sys,json;d=json.load(sys.stdin)['data']['list'];print([ (x['name'],x['status']) for x in d if x['name']=='$s'])")"
done
GATEWAY_DRIVER=$(echo "$SVCS" | python -c "import sys,json;d=json.load(sys.stdin)['data']['list'];print([x['driver'] for x in d if x['name']=='gateway'][0])")
[ "$GATEWAY_DRIVER" = "none" ] && ok "gateway 仅观测（driver=none）" || bad "gateway driver 异常: $GATEWAY_DRIVER"

say "菜单与前端落点"
MENUS=$(req GET /auth/profile | python -c "
import sys,json
ms=json.load(sys.stdin)['data']['menus']
def walk(xs):
    out=[]
    for x in xs or []:
        out.append(x.get('path',''))
        out+=walk(x.get('children'))
    return out
print('\n'.join(walk(ms)))")
echo "$MENUS" | grep -q "/system/ops" && ok "菜单树含 系统运维（/system/ops）" || bad "菜单树缺 /system/ops"

say "启停控制 + 审计 + 告警生命周期"
req POST /ops/services/integration/stop | python -c "import sys,json;d=json.load(sys.stdin);raise SystemExit(0 if d.get('code')==0 else 1)" && ok "停止 integration 指令受理" || bad "停止指令失败"
sleep 13  # 等一个健康轮询周期（10s）+ 缓冲
DOWN=$(req GET /ops/services | python -c "import sys,json;d=json.load(sys.stdin)['data']['list'];print([x['status'] for x in d if x['name']=='integration'][0])")
[ "$DOWN" = "down" ] && ok "integration 状态=down" || bad "integration 状态=$DOWN（期望 down）"
OPEN_AL=$(req GET "/ops/alerts?open=1" | python -c "import sys,json;d=json.load(sys.stdin)['data']['list'];print(len([x for x in d if x['service']=='integration']))")
[ "${OPEN_AL:-0}" -ge 1 ] && ok "告警已开单（service_down）" || bad "无未恢复告警"

# 故障隔离：核心流转不受影响（建单→下达→报工）
MAT=$(req GET "/materials?category=semi&page_size=1" | python -c "import sys,json;print(json.load(sys.stdin)['data']['list'][0]['id'])")
WO=$(req POST /work-orders "{\"materialId\":$MAT,\"qty\":50}" | python -c "import sys,json;print(json.load(sys.stdin)['data']['id'])")
req POST "/work-orders/$WO/release" | python -c "import sys,json;raise SystemExit(0 if json.load(sys.stdin).get('code')==0 else 1)" && ok "故障隔离：工单创建+下达正常" || bad "工单下达失败"
OP=$(req GET "/wo-operations?work_order_id=$WO" | python -c "import sys,json;print(json.load(sys.stdin)['data']['list'][0]['id'])")
RP=$(req POST "/production-reports" "{\"workOrderId\":$WO,\"operationId\":$OP,\"qtyOk\":10,\"qtyDefect\":0,\"hours\":1,\"clientToken\":\"fi-$WO\"}")
echo "$RP" | python -c "import sys,json;d=json.load(sys.stdin);raise SystemExit(0 if d.get('code')==0 else 1)" && ok "故障隔离：报工正常" || bad "报工失败: $RP"

req POST /ops/services/integration/start | python -c "import sys,json;raise SystemExit(0 if json.load(sys.stdin).get('code')==0 else 1)" && ok "启动 integration 指令受理" || bad "启动指令失败"
sleep 13
UP=$(req GET /ops/services | python -c "import sys,json;d=json.load(sys.stdin)['data']['list'];print([x['status'] for x in d if x['name']=='integration'][0])")
[ "$UP" = "up" ] && ok "integration 恢复 up" || bad "integration 状态=$UP（期望 up）"
RESOLVED=$(req GET /ops/alerts | python -c "import sys,json;d=json.load(sys.stdin)['data']['list'];print(len([x for x in d if x['service']=='integration' and x.get('resolvedAt')]))")
[ "${RESOLVED:-0}" -ge 1 ] && ok "告警已闭环（resolved）" || bad "告警未闭环"
AUDIT=$(req GET /ops/audit | python -c "import sys,json;d=json.load(sys.stdin)['data']['list'];print(len([x for x in d if x['service']=='integration' and x['action'] in ('stop','start') and x['result']=='ok']))")
[ "${AUDIT:-0}" -ge 2 ] && ok "运维审计 2 条（stop/start）" || bad "审计记录不足: $AUDIT"

printf '\n\033[1m结果: %d 通过 / %d 失败\033[0m\n' "$PASS" "$FAIL"
[ $FAIL = 0 ]
