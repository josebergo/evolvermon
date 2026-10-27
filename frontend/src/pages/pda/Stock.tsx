import { useEffect, useState } from 'react'
import { Button, Card, Input, InputNumber, List, Segmented, Space, Tag, message } from 'antd'
import { get, post } from '../../api/client'
import { useAuth } from '../../stores/auth'

// PDA 仓储作业：待办单据列表 → 扫码选单 → 逐行确认收货/领料（US-WMS-01 十五秒收货）
export default function PdaStock() {
  const [docs, setDocs] = useState<any[]>([])
  const [tab, setTab] = useState<'rcv' | 'shp'>('rcv')
  const [current, setCurrent] = useState<any>(null)
  const [lines, setLines] = useState<any[]>([])
  const [mats, setMats] = useState<any[]>([])
  const token = useAuth((s) => s.accessToken)

  const reload = () => get('/stock-orders', { doc_type: tab, page_size: 50, status: '' })
    .then((d: any) => setDocs((d.list || []).filter((o: any) => o.status === 'draft' || o.status === 'doing')))
    .catch(() => {})
  useEffect(() => { reload() }, [tab])
  useEffect(() => { get('/materials', { page_size: 200 }).then((d: any) => setMats(d.list || [])).catch(() => {}) }, [])
  if (!token) return <div style={{ padding: 24 }}><Card>请先<a href="/login">登录</a>后使用 PDA。</Card></div>

  const matName = (id: number) => {
    const m = mats.find((x) => x.id === id)
    return m ? `${m.code} ${m.name}` : id
  }

  const open = async (o: any) => {
    const full: any = await get(`/stock-orders/${o.id}`)
    setCurrent(full)
    setLines((full.lines || []).filter((l: any) => l.qty - l.doneQty > 1e-9).map((l: any) => ({ ...l, postQty: l.qty - l.doneQty, batchNo: l.batchNo || '' })))
  }

  const submitAll = async () => {
    const payload = {
      lines: lines.map((l) => ({ lineId: l.id, qty: l.postQty, batchNo: l.batchNo || undefined })),
    }
    await post(`/stock-orders/${current.id}/post-lines`, payload)
    message.success('过账成功 ✓')
    setCurrent(null)
    reload()
  }

  return (
    <div style={{ padding: 12, maxWidth: 720, margin: '0 auto' }}>
      <Card title="PDA 仓储作业" size="small" extra={<a href="/dashboard">返回 PC 端</a>}>
        <Segmented block value={tab} onChange={(v) => setTab(v as any)}
          options={[{ value: 'rcv', label: '收货入库' }, { value: 'shp', label: '领料/发货' }]} />
        {!current && (
          <List size="small" dataSource={docs} locale={{ emptyText: '暂无待办单据' }}
            renderItem={(o: any) => (
              <List.Item actions={[<Button key="a" size="small" type="primary" onClick={() => open(o)}>执行</Button>]}>
                <List.Item.Meta title={`${o.docNo}（${o.sourceDocNo || o.sourceType}）`}
                  description={<Tag color={o.status === 'draft' ? 'default' : 'processing'}>{o.status === 'draft' ? '未开始' : '执行中'}</Tag>} />
              </List.Item>
            )} />
        )}
        {current && (
          <>
            <div style={{ margin: '8px 0', fontSize: 16 }}>
              <b>{current.docNo}</b> <Tag>{current.docType === 'rcv' ? '入库' : '出库'}</Tag>
              <Button size="small" style={{ marginLeft: 8 }} onClick={() => setCurrent(null)}>返回列表</Button>
            </div>
            {lines.map((l) => (
              <div key={l.id} style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
                <span style={{ flex: 1, fontSize: 15 }}>{matName(l.materialId)}</span>
                <span style={{ color: '#888' }}>剩 {l.qty - l.doneQty}</span>
                <InputNumber size="large" min={0.01} max={l.qty - l.doneQty} value={l.postQty}
                  onChange={(v) => setLines(lines.map((x) => (x.id === l.id ? { ...x, postQty: v } : x)))} />
                <Input size="large" placeholder="批次" style={{ width: 110 }} value={l.batchNo}
                  onChange={(e) => setLines(lines.map((x) => (x.id === l.id ? { ...x, batchNo: e.target.value } : x)))} />
              </div>
            ))}
            <Button type="primary" size="large" block className="pda-btn" onClick={submitAll}>确认过账</Button>
            <div style={{ color: '#999', fontSize: 12, marginTop: 8 }}>出库未填批次时按先进先出自动拣批</div>
          </>
        )}
      </Card>
    </div>
  )
}
