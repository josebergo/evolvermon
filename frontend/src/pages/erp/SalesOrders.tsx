import { useEffect, useState } from 'react'
import { Button, Card, DatePicker, Form, Input, InputNumber, Modal, Select, Space, Table, Tag, message } from 'antd'
import { get, post, put } from '../../api/client'

const statusMap: Record<string, { text: string; color: string }> = {
  draft: { text: '草稿', color: 'default' }, audited: { text: '已审核', color: 'blue' },
  partial: { text: '部分发货', color: 'orange' }, done: { text: '已完成', color: 'green' },
  closed: { text: '已关闭', color: 'default' }, void: { text: '已作废', color: 'red' },
}

export default function SalesOrders() {
  const [rows, setRows] = useState<any[]>([])
  const [mats, setMats] = useState<any[]>([])
  const [custs, setCusts] = useState<any[]>([])
  const [editing, setEditing] = useState<any>(null)
  const [avail, setAvail] = useState<Record<number, number>>({})
  const [form] = Form.useForm()
  const [q, setQ] = useState('')

  const reload = async () => setRows((await get('/sales-orders', { page_size: 50, q: q || undefined }))?.list || [])
  useEffect(() => {
    reload()
    get('/materials', { page_size: 200 }).then((d: any) => setMats(d.list || []))
    get('/customers', { page_size: 100 }).then((d: any) => setCusts(d.list || []))
  }, [])
  useEffect(() => { reload() }, [q])

  const openEdit = (r?: any) => {
    const data = r ? { ...r } : { customerId: custs[0]?.id, lines: [{ qty: 1, price: 100 }] }
    setEditing(data)
    form.setFieldsValue(data)
    // FR-ERP-053：开单展示可承诺量（现有−预占）
    get('/stocks/available').then((rows: any) => {
      const m: Record<number, number> = {}
      for (const row of rows || []) m[row.materialId] = row.available
      setAvail(m)
    }).catch(() => {})
  }

  const save = async () => {
    const v = await form.validateFields()
    const payload = {
      customerId: Number(v.customerId),
      lines: (v.lines || []).map((l: any) => ({
        materialId: Number(l.materialId), qty: l.qty, price: l.price,
        amount: l.qty * l.price, deliveryDate: l.deliveryDate?.$d,
      })),
      amount: (v.lines || []).reduce((s: number, l: any) => s + l.qty * l.price, 0),
      deliveryDate: v.deliveryDate?.$d, remark: v.remark,
    }
    if (editing?.id) await put(`/sales-orders/${editing.id}`, { ...editing, ...payload })
    else await post('/sales-orders', payload)
    setEditing(null); reload()
  }

  return (
    <Card title="销售订单（审核后预占库存，可发起发货）" extra={
      <Space>
        <Input.Search placeholder="单号" allowClear style={{ width: 200 }} onSearch={setQ} />
        <Button onClick={reload}>刷新</Button>
        <Button type="primary" onClick={() => openEdit()}>新建订单</Button>
      </Space>
    }>
      <Table rowKey="id" size="middle" dataSource={rows} pagination={{ pageSize: 15 }}
        columns={[
          { title: '单号', dataIndex: 'docNo', width: 150 },
          { title: '客户', dataIndex: 'customerId', width: 140, render: (v) => custs.find((c) => c.id === v)?.name || v },
          { title: '金额', dataIndex: 'amount', width: 100 },
          { title: '状态', dataIndex: 'status', width: 100, render: (v) => <Tag color={statusMap[v]?.color}>{statusMap[v]?.text || v}</Tag> },
          { title: '交期', dataIndex: 'deliveryDate', width: 110, render: (v) => v?.slice(0, 10) },
          { title: '备注', dataIndex: 'remark', ellipsis: true },
          {
            title: '操作', width: 260, render: (_, r) => (
              <Space size={4}>
                <a onClick={() => openEdit(r)}>编辑</a>
                {r.status === 'draft' && <a onClick={async () => { await post(`/sales-orders/${r.id}/audit`); message.success('已审核并预占库存'); reload() }}>审核</a>}
                {(r.status === 'audited' || r.status === 'partial') && (
                  <a onClick={async () => {
                    const o = await post(`/sales-orders/${r.id}/create-shipment`)
                    message.success(`已生成发货出库单 ${o.docNo}，请到仓储管理执行过账`)
                  }}>生成发货单</a>
                )}
                {['draft', 'audited', 'partial'].includes(r.status) && (
                  <a style={{ color: '#cf1322' }} onClick={async () => { await post(`/sales-orders/${r.id}/void`); message.success('已作废'); reload() }}>作废</a>
                )}
              </Space>
            ),
          },
        ]} />

      <Modal title={editing?.id ? `编辑 ${editing.docNo}` : '新建销售订单'} open={!!editing} onOk={save}
        onCancel={() => setEditing(null)} width={860} destroyOnClose>
        <Form form={form} layout="vertical">
          <Space size={16}>
            <Form.Item name="customerId" label="客户" rules={[{ required: true }]}>
              <Select style={{ width: 260 }} showSearch optionFilterProp="label"
                options={custs.map((c) => ({ value: c.id, label: c.name }))} />
            </Form.Item>
            <Form.Item name="deliveryDate" label="交货日期"><DatePicker /></Form.Item>
            <Form.Item name="remark" label="备注" style={{ width: 260 }}><Input /></Form.Item>
          </Space>
          <Form.List name="lines">
            {(fields, { add, remove }) => (
              <>
                {fields.map((f) => (
                  <Space key={f.key} align="baseline" style={{ display: 'flex', marginBottom: 4 }}>
                    <Form.Item name={[f.name, 'materialId']} rules={[{ required: true, message: '选物料' }]} noStyle>
                      <Select showSearch placeholder="物料" style={{ width: 340 }} optionFilterProp="label"
                        options={mats.filter((m) => m.category === 'finished' || m.category === 'semi')
                          .map((m) => ({ value: m.id, label: `${m.code} ${m.name}（可用 ${avail[m.id] ?? 0}）` }))} />
                    </Form.Item>
                    <Form.Item name={[f.name, 'qty']} rules={[{ required: true }]} noStyle>
                      <InputNumber placeholder="数量" min={0.01} style={{ width: 110 }} />
                    </Form.Item>
                    <Form.Item name={[f.name, 'price']} noStyle>
                      <InputNumber placeholder="单价" min={0} style={{ width: 100 }} />
                    </Form.Item>
                    <Form.Item name={[f.name, 'deliveryDate']} noStyle><DatePicker placeholder="行交期" /></Form.Item>
                    <a onClick={() => remove(f.name)}>删除</a>
                  </Space>
                ))}
                <Button block onClick={() => add({ qty: 1, price: 0 })}>＋ 添加行</Button>
              </>
            )}
          </Form.List>
        </Form>
      </Modal>
    </Card>
  )
}
