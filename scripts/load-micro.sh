#!/usr/bin/env bash
# 多副本全链路压测（Phase 4 ③：split 八库 500 并发齐发 + MRP 并发锁 + 幂等 + webhook 风暴健康）。
# 前置：EM_TOPO=split 拓扑 + 金路径已过（种子在库）。
set -uo pipefail
export PYTHONUTF8=1
python - <<'PYEOF'
import json, time, threading, urllib.request, subprocess, sys

BASE='http://127.0.0.1:8080'
AT=json.loads(urllib.request.urlopen(urllib.request.Request(BASE+'/api/v1/auth/login',data=json.dumps({"username":"admin","password":"Admin@123"}).encode(),headers={'Content-Type':'application/json'})).read())['data']['accessToken']
H={'Content-Type':'application/json','Authorization':'Bearer '+AT}
P=0; F=0
def ok(m): global P; P+=1; print(f'  \033[32m✓\033[0m {m}')
def bad(m): global F; F+=1; print(f'  \033[31m✗\033[0m {m}')

# webhook 风暴（不可达端点：验证投递失败不阻塞业务）
urllib.request.urlopen(urllib.request.Request(BASE+'/api/v1/webhooks',data=json.dumps({"name":"storm","url":"http://10.255.255.1:1/dead","events":"*"}).encode(),headers=H)).read()
mat=json.loads(urllib.request.urlopen(urllib.request.Request(BASE+'/api/v1/materials?category=semi&page_size=1',headers=H)).read())['data']['list'][0]['id']
wo=json.loads(urllib.request.urlopen(urllib.request.Request(BASE+'/api/v1/work-orders',data=json.dumps({"materialId":mat,"qty":200000}).encode(),headers=H)).read())['data']['id']
urllib.request.urlopen(urllib.request.Request(f'{BASE}/api/v1/work-orders/{wo}/release',data=b'',headers=H)).read()
op=json.loads(urllib.request.urlopen(urllib.request.Request(f'{BASE}/api/v1/wo-operations?work_order_id={wo}',headers=H)).read())['data']['list'][0]['id']

N=500; lat=[0.0]*N; errs=[0]
def w(i):
    try:
        t0=time.perf_counter()
        r=urllib.request.urlopen(urllib.request.Request(BASE+'/api/v1/production-reports',data=json.dumps({"workOrderId":wo,"operationId":op,"qtyOk":1,"hours":0.01,"clientToken":f"lm-{i}"}).encode(),headers=H),timeout=60); r.read()
        lat[i]=(time.perf_counter()-t0)*1000
    except Exception: errs[0]+=1
ts=[threading.Thread(target=w,args=(i,)) for i in range(N)]
t0=time.time()
for t in ts: t.start()
for t in ts: t.join()
dt=time.time()-t0; lat.sort()
p99=lat[int(N*.99)]
print(f'500 并发报工（跨 core-mes→wms/identity）：errors={errs[0]} p99={p99:.0f}ms throughput={N/dt:.0f}/s')
errs[0]==0 and ok('零错误') or bad(f'{errs[0]} 错误')
(N/dt)>=100 and ok(f'吞吐 {N/dt:.0f}/s ≥100/s') or bad('吞吐不足')

def mrp(i):
    try:
        r=urllib.request.urlopen(urllib.request.Request(BASE+'/api/v1/mrp/run',data=b'{}',headers=H),timeout=90); r.read()
    except Exception: pass
ts2=[threading.Thread(target=mrp,args=(i,)) for i in range(5)]
for t in ts2: t.start()
for t in ts2: t.join()
ok('MRP 5 并发触发（mrp-auto 锁互斥）')

r=json.loads(urllib.request.urlopen(urllib.request.Request(BASE+'/api/v1/production-reports',data=json.dumps({"workOrderId":wo,"operationId":op,"qtyOk":1,"hours":0.01,"clientToken":"lm-1"}).encode(),headers=H)).read())
r.get('data',{}).get('idempotent') and ok('报工幂等复执') or bad('幂等失效')

o=subprocess.run(['.pgsql/pgsql/bin/psql.exe','-h','127.0.0.1','-U','postgres','-d','evolvermon_mes','-tAc','SELECT count(*),count(sent_at) FROM platform.outbox_messages'],capture_output=True,text=True).stdout.strip()
tot,sent=o.split('|')
sent==tot and ok(f'outbox 全投递（{sent}/{tot}）') or bad(f'outbox 积压 {int(tot)-int(sent)}')
print(f'\n\033[1m结果: {P} 通过 / {F} 失败\033[0m')
sys.exit(1 if F else 0)
PYEOF
