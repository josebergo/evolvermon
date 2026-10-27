import { useEffect, useState } from 'react'
import { Button, Card, Drawer, InputNumber, Modal, Select, Space, Table, Tag, message } from 'antd'
import { get, post } from '../../api/client'

// 盘点：创建快照 → 录入实盘 → 差异调整（FR-WMS-050~053）
export default function Stocktakings() {
  const [rows, setRows] = useState<any[]>([])
  const [whs, setWhs] = useState<any[]>([])
  const [mats, setMats] = useState<any[]>([])
  const [creating, setCreating] = useState(false)
  const [whId, setWhId] = useState<number | undefined>()
  const [method, setMethod] = useState('blind')
  const [detail, setDetail] = useState<any>(null)

  const reload = async () => setRows((await get('/stocktakings', { page_size: 50 }))?.list || [])
  useEffect(() => {
    reload()
    get('/warehouses', { page_size: 100 }).then((d: any) => setWhs(d.list || []))
    get('/materials', { page_size: 200 }).then((d: any) => setMats(d.list || []))
  }, [])

  const matName = (id: number) => {
    const m = mats.find((x) => x.id === id)
    return m ? `${m.code} ${m.name}` : id
  }

  const openDetail = async (r: any) => setDetail(await get(`/stocktakings/${r.id}`))

  return (
    <Card title="盘点管理（快照口径，差异自动生成盘盈/盘亏单）" extra={<Space>
      <Button onClick={reload}>刷新</Button>
      <Button type="primary" onClick={() => { setWhId(whs[0]?.id); setCreating(true) }}>新建盘点</Button>
    </Space>}>
      <Table rowKey="id" size="middle" dataSource={rows} pagination={{ pageSize: 15 }}
        columns={[
          { title: '单号', dataIndex: 'docNo', width: 150 },
          { title: '仓库', dataIndex: 'warehouseId', width: 140, render: (v) => whs.find((w) => w.id === v)?.name || v },
          { title: '方式', dataIndex: 'method', width: 80, render: (v) => (v === 'blind' ? '盲盘' : '明盘') },
          { title: '状态', dataIndex: 'status', width: 90, render: (v) => <Tag>{({ draft: '草稿', counting: '盘点中', confirmed: '差异确认', adjusted: '已调整' } as any)[v] || v}</Tag> },
          { title: '行数', dataIndex: 'lines', width: 70, render: (v: any[]) => v?.length || 0 },
          {
            title: '操作', width: 220, render: (_, r) => (
              <Space size={4}>
                <a onClick={() => openDetail(r)}>明细</a>
                {r.status === 'draft' && <a onClick={async () => { await post(`/stocktakings/${r.id}/start`); message.success('已开始盘点'); reload() }}>开始</a>}
                {r.status === 'counting' && <a onClick={async () => {
                  const res: any = await post(`/stocktakings/${r.id}/finish`)
                  message.success(`差异确认完成，生成盘盈行 ${res.gainOrders}、盘亏行 ${res.lossOrders}（调整单在出入库列表过账）`)
                  reload()
                }}>差异确认</a>}
              </Space>
            ),
          },
        ]} />

      <Modal title="新建盘点单（自动快照当前账面）" open={creating} onOk={async () => {
        await post('/stocktakings', { warehouseId: whId, scope: 'full', method })
        setCreating(false); reload()
      }} onCancel={() => setCreating(false)}>
        <Space>
          <Select placeholder="仓库" style={{ width: 220 }} value={whId} onChange={setWhId}
            options={whs.map((w) => ({ value: w.id, label: w.name }))} />
          <Select style={{ width: 100 }} value={method} onChange={setMethod}
            options={[{ value: 'blind', label: '盲盘' }, { value: 'open', label: '明盘' }]} />
        </Space>
      </Modal>

      <Drawer title={`盘点单 ${detail?.docNo || ''}（${detail?.method === 'blind' ? '盲盘：不显示账面' : '明盘'}）`}
        open={!!detail} onClose={() => setDetail(null)} width={680}>
        <Table rowKey="id" size="small" pagination={false} dataSource={detail?.lines || []}
          columns={[
            { title: '物料', dataIndex: 'materialId', render: (v) => matName(Number(v)) },
            { title: '批次', dataIndex: 'batchNo', width: 110, render: (v) => v || '—' },
            ...(detail?.method === 'open' ? [{ title: '账面', dataIndex: 'bookQty', width: 80 } as any] : []),
            { title: '实盘', width: 120, render: (_: any, l: any) => detail.status === 'counting' ? (
              <InputNumber size="small" min={0} value={l.countedQty ?? undefined}
                onChange={async (v: any) => {
                  if (v === null) return
                  await post(`/stocktakings/${detail.id}/lines/${l.id}`, { countedQty: v })
                  openDetail({ ...detail })
                }} />
            ) : (l.countedQty ?? '—') },
            { title: '差异', width: 80, render: (_: any, l: any) => {
              if (l.countedQty == null) return '—'
              const d = l.countedQty - l.bookQty
              return d === 0 ? '0' : <Tag color={d > 0 ? 'green' : 'red'}>{d > 0 ? '+' : ''}{Number(d.toFixed(3))}</Tag>
            } },
          ]} />
      </Drawer>
    </Card>
  )
}
