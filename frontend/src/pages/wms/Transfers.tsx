import { useEffect, useState } from 'react'
import { Button, Card, Form, Input, InputNumber, Select, Space, Table, Tag, message } from 'antd'
import { get, post } from '../../api/client'

// 厂间调拨（M5）：一步完成两厂库存划转，双单据留痕
export default function Transfers() {
  const [mats, setMats] = useState<any[]>([])
  const [facs, setFacs] = useState<any[]>([])
  const [form] = Form.useForm()
  const [logs, setLogs] = useState<any[]>([])

  const reload = () => get('/stock-orders?source_type=move&page_size=20').then((d: any) => setLogs(d.list || []))
  useEffect(() => {
    get('/materials?page_size=200').then((d: any) => setMats(d.list || []))
    get('/factories?page_size=20').then((d: any) => setFacs(d.list || []))
    reload()
  }, [])

  const submit = async () => {
    const v = await form.validateFields()
    const r: any = await post('/transfers', {
      fromFactoryId: Number(v.from), toFactoryId: Number(v.to),
      materialId: Number(v.materialId), qty: v.qty, batchNo: v.batchNo || '',
    })
    message.success(`调拨完成：调出 ${r.outDocNo} → 调入 ${r.inDocNo}`)
    form.resetFields(); reload()
  }

  return (
    <div>
      <Card title="厂间调拨（跨厂库存划转，双方各留一张已完成单据与流水）">
        <Form form={form} layout="inline" onFinish={submit}>
          <Form.Item name="from" label="调出厂" rules={[{ required: true }]}>
            <Select style={{ width: 150 }} options={facs.map((f) => ({ value: f.id, label: f.name }))} />
          </Form.Item>
          <Form.Item name="to" label="调入厂" rules={[{ required: true }]}>
            <Select style={{ width: 150 }} options={facs.map((f) => ({ value: f.id, label: f.name }))} />
          </Form.Item>
          <Form.Item name="materialId" label="物料" rules={[{ required: true }]}>
            <Select style={{ width: 220 }} showSearch optionFilterProp="label"
              options={mats.map((m) => ({ value: m.id, label: `${m.code} ${m.name}` }))} />
          </Form.Item>
          <Form.Item name="qty" label="数量" rules={[{ required: true }]}>
            <InputNumber min={0.01} />
          </Form.Item>
          <Form.Item name="batchNo" label="批次(可选)"><Input style={{ width: 120 }} /></Form.Item>
          <Button type="primary" htmlType="submit">执行调拨</Button>
        </Form>
      </Card>
      <Card title="调拨记录" style={{ marginTop: 12 }} styles={{ body: { padding: 12 } }}>
        <Table rowKey="id" size="small" dataSource={logs} pagination={{ pageSize: 10 }}
          columns={[
            { title: '单号', dataIndex: 'docNo', width: 150 },
            { title: '类型', dataIndex: 'docType', width: 80, render: (v) => <Tag color={v === 'rcv' ? 'green' : 'blue'}>{v === 'rcv' ? '调入' : '调出'}</Tag> },
            { title: '状态', dataIndex: 'status', width: 90, render: (v) => <Tag color="green">{v === 'done' ? '已完成' : v}</Tag> },
            { title: '备注', dataIndex: 'remark' },
            { title: '时间', dataIndex: 'createdAt', width: 160, render: (v) => v?.slice(0, 19).replace('T', ' ') },
          ]} />
      </Card>
    </div>
  )
}
