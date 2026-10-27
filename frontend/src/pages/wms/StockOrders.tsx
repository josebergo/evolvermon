import { useEffect, useState } from 'react'
import { Button, Card, Input, InputNumber, Modal, Select, Space, Table, Tag, message } from 'antd'
import { get, post } from '../../api/client'

// 出入库单（核心：分次过账，防超卖）FR-WMS-020~038
const docText: Record<string, string> = { rcv: '入库', shp: '出库', mv: '移库' }
const srcText: Record<string, string> = {
  purchase: '采购', production: '生产', sales: '销售', picking: '领料',
  scrap: '报废', gain: '盘盈', loss: '盘亏', move: '移库', manual: '手工',
}

export default function StockOrders() {
  const [rows, setRows] = useState<any[]>([])
  const [mats, setMats] = useState<any[]>([])
  const [whs, setWhs] = useState<any[]>([])
  const [docType, setDocType] = useState<string | undefined>()
  const [posting, setPosting] = useState<any>(null)
  const [postLines, setPostLines] = useState<any[]>([])
  const [creating, setCreating] = useState(false)
  const [createForm, setCreateForm] = useState<any>({ docType: 'shp', sourceType: 'manual', lines: [] })

  const reload = async () => setRows((await get('/stock-orders', { page_size: 50, doc_type: docType }))?.list || [])
  useEffect(() => { reload() }, [docType])
  useEffect(() => {
    get('/materials', { page_size: 200 }).then((d: any) => setMats(d.list || []))
    get('/warehouses', { page_size: 100 }).then((d: any) => setWhs(d.list || []))
  }, [])

  const matName = (id: number) => {
    const m = mats.find((x) => x.id === id)
    return m ? `${m.code} ${m.name}` : id
  }
  const whName = (id: number) => whs.find((w) => w.id === id)?.name || id

  const openPost = (r: any) => {
    setPosting(r)
    setPostLines((r.lines || []).filter((l: any) => l.qty - l.doneQty > 1e-9).map((l: any) => ({ ...l, postQty: l.qty - l.doneQty })))
  }

  const submitPost = async () => {
    await post(`/stock-orders/${posting.id}/post-lines`, {
      lines: postLines.map((l) => ({ lineId: l.id, qty: l.postQty, batchNo: l.batchNo || undefined })),
    })
    message.success('过账成功')
    setPosting(null); reload()
  }

  const createManual = async () => {
    if (!createForm.lines.length) { message.warning('请添加行'); return }
    await post('/stock-orders', {
      docType: createForm.docType, sourceType: 'manual',
      warehouseId: Number(createForm.warehouseId),
      lines: createForm.lines.map((l: any) => ({ materialId: Number(l.materialId), qty: l.qty, batchNo: l.batchNo })),
    })
    setCreating(false); reload()
  }

  return (
    <Card title="出入库单（过账即写台账+流水，出库校验可用量）" extra={<Space>
      <Select allowClear placeholder="单据类型" style={{ width: 110 }} value={docType}
        onChange={setDocType} options={Object.entries(docText).map(([v, l]) => ({ value: v, label: l }))} />
      <Button onClick={reload}>刷新</Button>
      <Button type="primary" onClick={() => setCreating(true)}>手工单据</Button>
    </Space>}>
      <Table rowKey="id" size="middle" dataSource={rows} pagination={{ pageSize: 15 }}
        columns={[
          { title: '单号', dataIndex: 'docNo', width: 150 },
          { title: '类型', dataIndex: 'docType', width: 70, render: (v) => <Tag color={v === 'rcv' ? 'green' : v === 'shp' ? 'blue' : 'purple'}>{docText[v]}</Tag> },
          { title: '来源', dataIndex: 'sourceType', width: 80, render: (v) => srcText[v] || v },
          { title: '来源单号', dataIndex: 'sourceDocNo', width: 130 },
          { title: '仓库', dataIndex: 'warehouseId', width: 130, render: (v) => whName(Number(v)) },
          { title: '状态', dataIndex: 'status', width: 80, render: (v) => <Tag>{({ draft: '草稿', doing: '执行中', done: '已完成', void: '作废' } as any)[v] || v}</Tag> },
          { title: '明细', render: (_, r) => (r.lines || []).map((l: any) => `${matName(l.materialId)}×${l.qty}（已执行${l.doneQty}）`).join('，') },
          {
            title: '操作', width: 120, render: (_, r) => (
              <Space size={4}>
                {['draft', 'doing'].includes(r.status) && <a onClick={() => openPost(r)}>过账</a>}
                <a onClick={() => window.open(`/api/v1/print/stock-orders/${r.id}?access_token=${localStorage.getItem('em_at')}`, '_blank')}>打印</a>
              </Space>
            ),
          },
        ]} />

      <Modal title={`过账：${posting?.docNo || ''}`} open={!!posting} onOk={submitPost} onCancel={() => setPosting(null)} width={680}>
        {posting?.docType === 'rcv' && <div style={{ marginBottom: 8, color: '#888' }}>入库：填写批次（启用批次的物料必填）与数量。</div>}
        {posting?.docType === 'shp' && <div style={{ marginBottom: 8, color: '#888' }}>出库：数量不能超过可用量（现有−预占），系统防超卖。</div>}
        {postLines.map((l) => (
          <Space key={l.id} style={{ display: 'flex', marginBottom: 6 }}>
            <span style={{ width: 260 }}>{matName(l.materialId)}</span>
            <span style={{ width: 90 }}>未执行 {l.qty - l.doneQty}</span>
            <InputNumber min={0.01} max={l.qty - l.doneQty} value={l.postQty}
              onChange={(v) => setPostLines(postLines.map((x) => (x.id === l.id ? { ...x, postQty: v } : x)))} />
            <Input placeholder="批次(可选)" value={l.batchNo || ''}
              onChange={(e) => setPostLines(postLines.map((x) => (x.id === l.id ? { ...x, batchNo: e.target.value } : x)))} style={{ width: 120 }} />
          </Space>
        ))}
      </Modal>

      <Modal title="新建手工单据" open={creating} onOk={createManual} onCancel={() => setCreating(false)} width={680}>
        <Space style={{ marginBottom: 12 }}>
          <Select value={createForm.docType} style={{ width: 100 }}
            onChange={(v) => setCreateForm({ ...createForm, docType: v })}
            options={Object.entries(docText).map(([v, l]) => ({ value: v, label: l }))} />
          <Select placeholder="仓库" style={{ width: 200 }} showSearch optionFilterProp="label"
            value={createForm.warehouseId}
            onChange={(v) => setCreateForm({ ...createForm, warehouseId: v })}
            options={whs.map((w) => ({ value: w.id, label: w.name }))} />
        </Space>
        {createForm.lines.map((l: any, i: number) => (
          <Space key={i} style={{ display: 'flex', marginBottom: 6 }}>
            <Select showSearch placeholder="物料" style={{ width: 280 }} optionFilterProp="label"
              value={l.materialId}
              onChange={(v: any) => setCreateForm({ ...createForm, lines: createForm.lines.map((x: any, j: number) => (j === i ? { ...x, materialId: v } : x)) })}
              options={mats.map((m) => ({ value: m.id, label: `${m.code} ${m.name}` }))} />
            <InputNumber min={0.01} placeholder="数量" value={l.qty}
              onChange={(v: any) => setCreateForm({ ...createForm, lines: createForm.lines.map((x: any, j: number) => (j === i ? { ...x, qty: v } : x)) })} />
            <Input placeholder="批次(可选)" value={l.batchNo} style={{ width: 110 }}
              onChange={(e: any) => setCreateForm({ ...createForm, lines: createForm.lines.map((x: any, j: number) => (j === i ? { ...x, batchNo: e.target.value } : x)) })} />
            <a onClick={() => setCreateForm({ ...createForm, lines: createForm.lines.filter((_: any, j: number) => j !== i) })}>删</a>
          </Space>
        ))}
        <Button block onClick={() => setCreateForm({ ...createForm, lines: [...createForm.lines, {}] })}>＋ 添加行</Button>
      </Modal>
    </Card>
  )
}
