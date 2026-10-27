import { useEffect, useState } from 'react'
import { Button, Card, Form, InputNumber, Select, Space, Table, Tag, message } from 'antd'
import { get, post } from '../../api/client'
import { useAuth } from '../../stores/auth'

// PDA 报工（US-MES-01：扫码三秒报工；大按钮触屏布局）
export default function PdaReport() {
  const [ops, setOps] = useState<any[]>([])
  const [wos, setWos] = useState<any[]>([])
  const [selectedOp, setSelectedOp] = useState<any>(null)
  const [recent, setRecent] = useState<any[]>([])
  const [form] = Form.useForm()
  const token = useAuth((s) => s.accessToken)
  const loadProfile = useAuth((s) => s.loadProfile)

  useEffect(() => {
    if (!token) return
    loadProfile().then(() => {
      get('/wo-operations', { mine: '1' }).then((d: any) => setOps(d?.list || [])).catch(() => {})
      get('/production-reports', { mine: '1' }).then((d: any) => setRecent(d?.list || [])).catch(() => {})
      get('/work-orders', { page_size: 30 }).then((d: any) => setWos(d?.list || [])).catch(() => {})
    }).catch(() => {})
  }, [token])

  if (!token) {
    return <div style={{ padding: 24 }}><Card>请先<a href="/login">登录</a>后使用 PDA 报工。</Card></div>
  }

  const submit = async (v: any) => {
    if (!selectedOp) { message.warning('请先选择工序'); return }
    await post('/production-reports', {
      workOrderId: selectedOp.workOrderId, operationId: selectedOp.id,
      qtyOk: v.qtyOk, qtyDefect: v.qtyDefect || 0, defectCodes: v.defectCodes || [],
      hours: v.hours || 0, clientToken: crypto.randomUUID(),
    })
    message.success('报工成功 ✓')
    form.resetFields()
    get('/production-reports', { mine: '1' }).then((d: any) => setRecent(d?.list || []))
  }

  const woNo = (id: number) => wos.find((w) => w.id === id)?.docNo || id

  return (
    <div style={{ padding: 12, maxWidth: 720, margin: '0 auto' }}>
      <Card title="PDA 扫码报工" size="small" extra={<a href="/dashboard">返回 PC 端</a>}>
        <Select size="large" style={{ width: '100%', marginBottom: 12 }} placeholder="选择工单/工序（我的任务）"
          value={selectedOp?.id} showSearch optionFilterProp="label"
          onChange={(id) => setSelectedOp(ops.find((o) => o.id === id))}
          options={ops.filter((o) => o.status !== 'done').map((o) => ({
            value: o.id, label: `${woNo(o.workOrderId)} · ${o.seq}.${o.name}（${{ waiting: '待开工', assigned: '已派', running: '进行中' }[o.status as string] || o.status}）`,
          }))} />
        <Form form={form} layout="vertical" onFinish={submit}>
          <Space style={{ width: '100%' }}>
            <Form.Item name="qtyOk" label="合格数" rules={[{ required: true }]} style={{ marginBottom: 8 }}>
              <InputNumber size="large" min={0} style={{ width: 130 }} />
            </Form.Item>
            <Form.Item name="qtyDefect" label="不良数" initialValue={0} style={{ marginBottom: 8 }}>
              <InputNumber size="large" min={0} style={{ width: 130 }} />
            </Form.Item>
            <Form.Item name="hours" label="工时(h)" initialValue={0} style={{ marginBottom: 8 }}>
              <InputNumber size="large" min={0} step={0.5} style={{ width: 110 }} />
            </Form.Item>
          </Space>
          <Form.Item name="defectCodes" label="不良代码（不良数>0 必选）">
            <Select mode="multiple" size="large" options={['D001 外观划伤', 'D002 尺寸超差', 'D003 焊点虚焊', 'D004 功能不良', 'D005 来料不良']
              .map((s) => ({ value: s.split(' ')[0], label: s }))} />
          </Form.Item>
          <Button type="primary" size="large" block htmlType="submit" className="pda-btn">提交报工</Button>
        </Form>
      </Card>
      <Card title="我最近的报工" size="small" style={{ marginTop: 12 }}>
        <Table rowKey="id" size="small" pagination={{ pageSize: 5 }} dataSource={recent}
          columns={[
            { title: '时间', dataIndex: 'reportedAt', width: 100, render: (v) => v?.slice(5, 16).replace('T', ' ') },
            { title: '合格', dataIndex: 'qtyOk', width: 60 },
            { title: '不良', dataIndex: 'qtyDefect', width: 60, render: (v) => (v > 0 ? <Tag color="red">{v}</Tag> : 0) },
            { title: '代码', dataIndex: 'defectCodes' },
          ]} />
      </Card>
    </div>
  )
}
