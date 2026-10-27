import { useEffect, useState } from 'react'
import { Button, Card, Form, Input, Modal, Popconfirm, Select, Space, Table, Tag, message } from 'antd'
import { del, get, post } from '../../api/client'

// Webhook 事件出口（M6）：订阅领域事件，HMAC 签名投递
export default function Webhooks() {
  const [eps, setEps] = useState<any[]>([])
  const [logs, setLogs] = useState<any[]>([])
  const [creating, setCreating] = useState(false)
  const [form] = Form.useForm()

  const reload = () => {
    get('/webhooks').then(setEps)
    get('/webhook-deliveries?page_size=50').then((d: any) => setLogs(d.list || []))
  }
  useEffect(() => { reload() }, [])

  const save = async () => {
    const v = await form.validateFields()
    await post('/webhooks', { ...v, events: v.events?.join(',') || '*' })
    message.success('已创建')
    setCreating(false); form.resetFields(); reload()
  }

  return (
    <div>
      <Card title="Webhook 事件出口（工单下达/过账/质检判定/订单审核 等领域事件，HMAC-SHA256 签名推送）" extra={
        <Button type="primary" onClick={() => setCreating(true)}>新增订阅</Button>
      }>
        <Table rowKey="id" size="small" dataSource={eps} pagination={false}
          columns={[
            { title: '名称', dataIndex: 'name', width: 140 },
            { title: '回调 URL', dataIndex: 'url' },
            { title: '订阅事件', dataIndex: 'events', width: 200 },
            { title: '密钥', dataIndex: 'secret', width: 120, render: (v) => (v ? '已设置' : '—') },
            {
              title: '操作', width: 80, render: (_, r) => (
                <Popconfirm title="确认删除？" onConfirm={async () => { await del(`/webhooks/${r.id}`); reload() }}>
                  <a style={{ color: '#B3251B' }}>删除</a>
                </Popconfirm>
              ),
            },
          ]} />
      </Card>
      <Card title="投递日志（最近 50 条）" style={{ marginTop: 12 }} styles={{ body: { padding: 12 } }}>
        <Table rowKey="id" size="small" dataSource={logs} pagination={{ pageSize: 10 }}
          columns={[
            { title: '时间', dataIndex: 'createdAt', width: 160, render: (v) => v?.slice(0, 19).replace('T', ' ') },
            { title: '事件', dataIndex: 'event', width: 170, render: (v) => <Tag color="orange">{v}</Tag> },
            { title: '状态', dataIndex: 'status', width: 90, render: (v) => <Tag color={v === 'ok' ? 'green' : v === 'failed' ? 'red' : 'default'}>{v}</Tag> },
            { title: 'HTTP', dataIndex: 'httpCode', width: 70 },
            { title: '尝试', dataIndex: 'attempts', width: 60 },
            { title: '负载', dataIndex: 'payload', ellipsis: true },
          ]} />
      </Card>
      <Modal title="新增 Webhook 订阅" open={creating} onOk={save} onCancel={() => setCreating(false)} destroyOnClose>
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input placeholder="如 MES 大屏联动" /></Form.Item>
          <Form.Item name="url" label="回调 URL" rules={[{ required: true }]}><Input placeholder="https://..." /></Form.Item>
          <Form.Item name="events" label="订阅事件">
            <Select mode="multiple" allowClear placeholder="不选=全部事件" options={[
              { value: 'workorder.released', label: '工单下达' },
              { value: 'stock.posted', label: '库存过账' },
              { value: 'qc.audited', label: '质检判定' },
              { value: 'salesorder.audited', label: '销售订单审核' },
            ]} />
          </Form.Item>
          <Form.Item name="secret" label="签名密钥（HMAC-SHA256，可选）"><Input.Password /></Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
