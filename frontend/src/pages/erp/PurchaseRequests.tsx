import { useEffect, useState } from 'react'
import { Button, Card, Modal, Select, Space, Table, Tag, message } from 'antd'
import { get, post } from '../../api/client'

export default function PurchaseRequests() {
  const [rows, setRows] = useState<any[]>([])
  const [mats, setMats] = useState<any[]>([])
  const [supps, setSupps] = useState<any[]>([])
  const [conv, setConv] = useState<any[] | null>(null)
  const [supplierId, setSupplierId] = useState<number | undefined>()

  const reload = async () => setRows((await get('/purchase-requests', { page_size: 50 }))?.list || [])
  useEffect(() => {
    reload()
    get('/materials', { page_size: 200 }).then((d: any) => setMats(d.list || []))
    get('/suppliers', { page_size: 100 }).then((d: any) => setSupps(d.list || []))
  }, [])

  const matName = (id: number) => {
    const m = mats.find((x) => x.id === id)
    return m ? `${m.code} ${m.name}` : id
  }

  return (
    <Card title="请购单（MRP 建议或手工创建 → 转采购订单）" extra={<Space>
      <Button onClick={reload}>刷新</Button>
      <Button onClick={async () => {
        await post('/purchase-requests', { sourceType: 'manual', lines: [] })
        message.success('已创建空请购（可编辑行）'); reload()
      }}>新建请购</Button>
    </Space>}>
      <Table rowKey="id" size="middle" dataSource={rows} pagination={{ pageSize: 15 }}
        columns={[
          { title: '单号', dataIndex: 'docNo', width: 150 },
          { title: '来源', dataIndex: 'sourceType', width: 80, render: (v) => (v === 'mrp' ? 'MRP' : '手工') },
          { title: '状态', dataIndex: 'status', width: 100, render: (v) => <Tag>{({ draft: '草稿', converted: '已转采购', closed: '关闭', void: '作废' } as any)[v] || v}</Tag> },
          { title: '明细', render: (_, r) => (r.lines || []).map((l: any) => `${matName(l.materialId)}×${l.qty}`).join('，') },
          {
            title: '操作', width: 130, render: (_, r) => r.status === 'draft' ? (
              <a onClick={() => { setConv([r]); setSupplierId(supps[0]?.id) }}>转采购</a>
            ) : null,
          },
        ]} />

      <Modal title="转换为采购订单" open={!!conv} onOk={async () => {
        const res: any = await post('/purchase-requests/convert-to-po', { ids: conv!.map((r) => r.id), supplierId })
        message.success(`已生成采购订单 ${res.poDocNo}`)
        setConv(null); reload()
      }} onCancel={() => setConv(null)}>
        <div style={{ marginBottom: 12 }}>
          供应商：<Select style={{ width: 240 }} value={supplierId} onChange={setSupplierId}
            options={supps.map((s) => ({ value: s.id, label: s.name }))} />
        </div>
        <div>将合并选中请购的未采购数量生成一张 PO。</div>
      </Modal>
    </Card>
  )
}
