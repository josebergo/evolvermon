import { useEffect, useState } from 'react'
import { Button, Card, DatePicker, Form, Input, InputNumber, Modal, Select, Space, Table, Tag, message } from 'antd'
import { get, post } from '../../api/client'

const stMap: Record<string, string> = { draft: '草稿', arriving: '已审核', receiving: '到货中', partial: '部分入库', done: '已完成', closed: '已关闭', void: '已作废' }

export default function PurchaseOrders() {
  const [rows, setRows] = useState<any[]>([])
  const [mats, setMats] = useState<any[]>([])
  const [supps, setSupps] = useState<any[]>([])
  const [creating, setCreating] = useState(false)
  const [form] = Form.useForm()

  const reload = async () => setRows((await get('/purchase-orders', { page_size: 50 }))?.list || [])
  useEffect(() => {
    reload()
    get('/materials', { page_size: 200 }).then((d: any) => setMats(d.list || []))
    get('/suppliers', { page_size: 100 }).then((d: any) => setSupps(d.list || []))
  }, [])

  const matName = (id: number) => {
    const m = mats.find((x) => x.id === id)
    return m ? `${m.code} ${m.name}` : id
  }

  const save = async () => {
    const v = await form.validateFields()
    await post('/purchase-orders', {
      supplierId: Number(v.supplierId), orderDate: new Date().toISOString(),
      lines: (v.lines || []).map((l: any) => ({ materialId: Number(l.materialId), qty: l.qty, price: l.price, amount: l.qty * l.price })),
    })
    setCreating(false); form.resetFields(); reload()
  }

  return (
    <Card title="采购订单（审核 → 到货 → 生成入库单）" extra={<Space>
      <Button onClick={reload}>刷新</Button>
      <Button type="primary" onClick={() => setCreating(true)}>新建采购单</Button>
    </Space>}>
      <Table rowKey="id" size="middle" dataSource={rows} pagination={{ pageSize: 15 }}
        columns={[
          { title: '单号', dataIndex: 'docNo', width: 150 },
          { title: '供应商', dataIndex: 'supplierId', width: 150, render: (v) => supps.find((s) => s.id === v)?.name || v },
          { title: '金额', dataIndex: 'amount', width: 100 },
          { title: '状态', dataIndex: 'status', width: 90, render: (v) => <Tag>{stMap[v] || v}</Tag> },
          { title: '明细', render: (_, r) => (r.lines || []).map((l: any) => `${matName(l.materialId)}×${l.qty}（已入${l.receivedQty}）`).join('，') },
          {
            title: '操作', width: 150, render: (_, r) => (
              <Space size={4}>
                {r.status === 'draft' && <a onClick={async () => { await post(`/purchase-orders/${r.id}/audit`); message.success('已审核'); reload() }}>审核</a>}
                {(r.status === 'arriving' || r.status === 'receiving') && (
                  <a onClick={async () => {
                    const o = await post(`/purchase-orders/${r.id}/arrive`)
                    message.success(`已生成到货入库单 ${o.docNo}，请到仓储管理收货过账`)
                  }}>到货</a>
                )}
              </Space>
            ),
          },
        ]} />

      <Modal title="新建采购订单" open={creating} onOk={save} onCancel={() => setCreating(false)} width={800} destroyOnClose>
        <Form form={form} layout="vertical">
          <Form.Item name="supplierId" label="供应商" rules={[{ required: true }]}>
            <Select showSearch optionFilterProp="label" style={{ width: 300 }}
              options={supps.map((s) => ({ value: s.id, label: s.name }))} />
          </Form.Item>
          <Form.List name="lines">
            {(fields, { add, remove }) => (
              <>
                {fields.map((f) => (
                  <Space key={f.key} align="baseline">
                    <Form.Item name={[f.name, 'materialId']} rules={[{ required: true }]} noStyle>
                      <Select showSearch placeholder="物料" style={{ width: 300 }} optionFilterProp="label"
                        options={mats.filter((m) => m.category === 'raw').map((m) => ({ value: m.id, label: `${m.code} ${m.name}` }))} />
                    </Form.Item>
                    <Form.Item name={[f.name, 'qty']} rules={[{ required: true }]} noStyle><InputNumber placeholder="数量" min={0.01} style={{ width: 110 }} /></Form.Item>
                    <Form.Item name={[f.name, 'price']} noStyle><InputNumber placeholder="单价" min={0} style={{ width: 100 }} /></Form.Item>
                    <a onClick={() => remove(f.name)}>删除</a>
                  </Space>
                ))}
                <Button block onClick={() => add({})}>＋ 添加行</Button>
              </>
            )}
          </Form.List>
        </Form>
      </Modal>
    </Card>
  )
}
