import { useEffect, useState } from 'react'
import { Button, Card, Select, Space, Table, Tag } from 'antd'
import { get } from '../../api/client'

// 库存台账 + 可用量视图（FR-WMS-001~004）
export default function Stocks() {
  const [rows, setRows] = useState<any[]>([])
  const [whs, setWhs] = useState<any[]>([])
  const [mats, setMats] = useState<any[]>([])
  const [avail, setAvail] = useState<any[]>([])
  const [whFilter, setWhFilter] = useState<string | undefined>()

  const reload = async () => {
    const d: any = await get('/stocks', { warehouse_id: whFilter, positive: '1' })
    setRows(d?.list || [])
    const a: any = await get('/stocks/available')
    setAvail(a || [])
  }
  useEffect(() => { reload() }, [whFilter])
  useEffect(() => {
    get('/warehouses', { page_size: 100 }).then((d: any) => setWhs(d.list || []))
    get('/materials', { page_size: 200 }).then((d: any) => setMats(d.list || []))
  }, [])

  const matName = (id: number) => {
    const m = mats.find((x) => x.id === id)
    return m ? `${m.code} ${m.name}` : id
  }
  const whName = (id: number) => whs.find((w) => w.id === id)?.name || id

  return (
    <div>
      <Card title="库存台账（单据驱动，流水可穿透）" extra={<Space>
        <Select allowClear placeholder="全部仓库" style={{ width: 180 }} value={whFilter}
          onChange={setWhFilter} options={whs.map((w) => ({ value: String(w.id), label: w.name }))} />
        <Button onClick={reload}>刷新</Button>
      </Space>} styles={{ body: { padding: 12 } }}>
        <Table rowKey="id" size="middle" dataSource={rows} pagination={{ pageSize: 15 }}
          columns={[
            { title: '物料', dataIndex: 'materialId', render: (v) => matName(Number(v)) },
            { title: '仓库', dataIndex: 'warehouseId', width: 140, render: (v) => whName(Number(v)) },
            { title: '批次', dataIndex: 'batchNo', width: 120, render: (v) => v || '—' },
            { title: '现存量', dataIndex: 'qtyOnHand', width: 100 },
            { title: '预占', dataIndex: 'qtyReserved', width: 80 },
            { title: '不良', dataIndex: 'qtyDefective', width: 80 },
            { title: '更新时间', dataIndex: 'updatedAt', width: 160, render: (v) => v?.slice(0, 19).replace('T', ' ') },
          ]} />
      </Card>
      <Card title="物料可用量汇总（现有 − 预占，MRP/销售统一口径）" style={{ marginTop: 12 }} styles={{ body: { padding: 12 } }}>
        <Table rowKey="materialId" size="small" pagination={false} dataSource={avail}
          columns={[
            { title: '物料', dataIndex: 'materialId', render: (v) => matName(Number(v)) },
            { title: '现存量', dataIndex: 'onHand', width: 120 },
            { title: '预占', dataIndex: 'reserved', width: 100, render: (v) => (v > 0 ? <Tag color="orange">{v}</Tag> : 0) },
            { title: '可用量', dataIndex: 'available', width: 120, render: (v) => <b style={{ color: v > 0 ? '#3f8600' : '#cf1322' }}>{v}</b> },
          ]} />
      </Card>
    </div>
  )
}
