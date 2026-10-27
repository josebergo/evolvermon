import { useEffect, useState } from 'react'
import { Button, Card, Form, Input, InputNumber, Modal, Space, Table, Tag } from 'antd'
import { get, post } from '../../api/client'

// 设备遥测（M6）：推送式网关接口 —— 设备/采集盒通过此端点上报
export default function Telemetry() {
  const [rows, setRows] = useState<any[]>([])
  const [pushing, setPushing] = useState(false)
  const [form] = Form.useForm()

  const reload = () => get('/devices/telemetry/latest').then(setRows).catch(() => {})
  useEffect(() => {
    reload()
    const t = setInterval(reload, 10000)
    return () => clearInterval(t)
  }, [])

  const push = async () => {
    const v = await form.validateFields()
    await post(`/devices/${v.code}/telemetry`, { metric: v.metric, value: v.value, workCenter: v.workCenter || '' })
    setPushing(false); form.resetFields(); reload()
  }

  return (
    <div>
      <Card title="设备遥测（推送式采集：设备/网关 → 本接口 → 看板）" extra={<Space>
        <Button onClick={reload}>刷新</Button>
        <Button type="primary" onClick={() => setPushing(true)}>模拟上报</Button>
      </Space>}>
        <Table rowKey={(r: any) => `${r.deviceCode}-${r.metric}`} size="middle" dataSource={rows} pagination={false}
          columns={[
            { title: '设备编码', dataIndex: 'deviceCode', width: 130 },
            { title: '指标', dataIndex: 'metric', width: 120, render: (v) => <Tag color="orange">{v}</Tag> },
            { title: '最新值', dataIndex: 'value', width: 120, render: (v) => <b style={{ fontSize: 15 }}>{v}</b> },
            { title: '工作中心', dataIndex: 'workCenter' },
            { title: '上报时间', dataIndex: 'reportedAt', width: 170, render: (v) => v?.slice(0, 19).replace('T', ' ') },
          ]} />
        <div style={{ color: '#5C4433', marginTop: 8, fontSize: 12 }}>
          接入方式：POST /api/v1/devices/&#123;code&#125;/telemetry &#123;"metric":"count","value":123&#125;（登录态；批量协议网关 OPC-UA/Modbus 属后续探索项）
        </div>
      </Card>
      <Modal title="模拟设备上报" open={pushing} onOk={push} onCancel={() => setPushing(false)} destroyOnClose>
        <Form form={form} layout="vertical">
          <Form.Item name="code" label="设备编码" rules={[{ required: true }]}><Input placeholder="SMT-L1" /></Form.Item>
          <Form.Item name="metric" label="指标" rules={[{ required: true }]}><Input placeholder="count / temperature / oee" /></Form.Item>
          <Form.Item name="value" label="数值" rules={[{ required: true }]}><InputNumber style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="workCenter" label="工作中心(可选)"><Input /></Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
