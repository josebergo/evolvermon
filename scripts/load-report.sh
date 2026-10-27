#!/usr/bin/env bash
# 报工接口并发压测（NFR-001：单节点 200 并发，P99 < 300ms）
# 用法：bash scripts/load-report.sh [base_url] [并发数]
set -uo pipefail
export PYTHONUTF8=1
BASE="${1:-http://127.0.0.1:8080}/api/v1"
CONC="${2:-200}"
echo "== 报工压测：并发 $CONC =="

AT=$(curl -s -X POST -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"Admin@123"}' "$BASE/auth/login" \
  | python -c "import sys,json;print(json.load(sys.stdin)['data']['accessToken'])")

# 准备一张已下达工单（数量拉大以容纳并发报工）
MAT=$(curl -s -H "Authorization: Bearer $AT" "$BASE/materials?category=semi&page_size=1" | python -c "import sys,json;print(json.load(sys.stdin)['data']['list'][0]['id'])")
WO=$(curl -s -X POST -H "Authorization: Bearer $AT" -H "Content-Type: application/json" \
  -d "{\"materialId\":$MAT,\"qty\":100000}" "$BASE/work-orders" | python -c "import sys,json;print(json.load(sys.stdin)['data']['id'])")
curl -s -X POST -H "Authorization: Bearer $AT" "$BASE/work-orders/$WO/release" > /dev/null
OP=$(curl -s -H "Authorization: Bearer $AT" "$BASE/wo-operations?work_order_id=$WO" | python -c "import sys,json;print(json.load(sys.stdin)['data']['list'][0]['id'])")
echo "工单=$WO 工序=$OP"

# 并发 worker（独立脚本避免引号地狱）
cat > .load-worker.sh <<EOF
#!/usr/bin/env bash
curl -s -o /dev/null -X POST \
  -H "Authorization: Bearer $AT" -H "Content-Type: application/json" \
  -d "{\"workOrderId\":$WO,\"operationId\":$OP,\"qtyOk\":1,\"qtyDefect\":0,\"hours\":0.01,\"clientToken\":\"load-\$1\"}" \
  -w "%{time_total}\n" "$BASE/production-reports"
EOF
seq 1 "$CONC" | xargs -P "$CONC" -I{} bash .load-worker.sh {} > .load-times.txt

python - <<'EOF'
ts = sorted(float(x)*1000 for x in open('.load-times.txt') if x.strip())
n = len(ts)
if n == 0:
    print("NO RESULTS"); raise SystemExit(1)
p = lambda q: ts[min(n-1, int(n*q))]
print(f"样本 {n}  min {ts[0]:.0f}ms  p50 {p(0.5):.0f}ms  p95 {p(0.95):.0f}ms  p99 {p(0.99):.0f}ms  max {ts[-1]:.0f}ms")
print("NFR-001 (P99<300ms):", "PASS" if p(0.99) < 300 else "FAIL")
EOF
rm -f .load-times.txt .load-worker.sh
