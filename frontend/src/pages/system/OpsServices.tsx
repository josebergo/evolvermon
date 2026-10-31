import { useEffect, useState } from 'react'
import { Badge, Button, Card, Col, Popconfirm, Row, Space, Statistic, Table, Tag, Typography, message } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { client } from '../../api/client'

interface SvcState {
  name: string
  url: string
  driver: string
  status: 'up' | 'down' | 'unknown'
  ready: boolean
  lastCheckAt: string
  downSince?: string
  uptimeSeconds: number
  goroutines: number
  heapBytes: number
  requestsTotal: number
  p99BucketSeconds: number
}

interface Alert { id: number; service: string; rule: string; detail: string; openedAt: string; resolvedAt?: string }
interface AuditRow { id: number; username: string; service: string; action: string; result: string; detail: string; createdAt: string }

const statusBadge = (s: SvcState) =>
  s.status === 'up' ? <Badge status="success" text="运行中" />
    : s.status === 'down' ? <Badge status="error" text="不可用" />
      : <Badge status="default" text="未知" />

const fmtUptime = (sec: number) => {
  if (!sec) return '—'
  const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60)
  return h > 0 ? `${h}时${m}分` : `${m}分${Math.floor(sec % 60)}秒`
}

export default function OpsServices() {
  const [services, setServices] = useState<SvcState[]>([])
  const [alerts, setAlerts] = useState<Alert[]>([])
  const [audit, setAudit] = useState<AuditRow[]>([])
  const [loading, setLoading] = useState(false)

  const refresh = async () => {
    setLoading(true)
    try {
      const [sv, al, au] = await Promise.all([
        client.get('/ops/services') as any,
        client.get('/ops/alerts') as any,
        client.get('/ops/audit') as any,
      ])
      setServices(sv.list || [])
      setAlerts(al.list || [])
      setAudit(au.list || [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    refresh()
    const t = setInterval(refresh, 15000) // 15s 自动刷新（与指标抓取同频）
    return () => clearInterval(t)
  }, [])

  const control = async (name: string, action: string) => {
    try {
      await client.post(`/ops/services/${name}/${action}`)
      message.success(`${name} ${action} 已执行`)
      setTimeout(refresh, 1500)
    } catch {
      /* 错误提示由拦截器统一弹出 */
    }
  }

  return (
    <div style={{ padding: 4 }}>
      <Space style={{ marginBottom: 16 }}>
        <Typography.Title level={4} style={{ margin: 0 }}>系统运维 · 服务总览</Typography.Title>
        <Button icon={<ReloadOutlined />} onClick={refresh} loading={loading}>刷新</Button>
      </Space>

      <Row gutter={[12, 12]}>
        {services.map((s) => (
          <Col key={s.name} xs={24} sm={12} lg={8} xl={6}>
            <Card
              size="small"
              title={<Space>{statusBadge(s)}<b>{s.name}</b>{s.driver === 'none' && <Tag>仅观测</Tag>}</Space>}
              extra={
                s.driver !== 'none' && (
                  <Space size={4}>
                    <Popconfirm title={`启动 ${s.name}？`} onConfirm={() => control(s.name, 'start')}>
                      <Button size="small" disabled={s.status === 'up'}>启动</Button>
                    </Popconfirm>
                    <Popconfirm title={`停止 ${s.name}？业务可能受影响`} onConfirm={() => control(s.name, 'stop')}>
                      <Button size="small" danger disabled={s.status !== 'up'}>停止</Button>
                    </Popconfirm>
                    <Popconfirm title={`重启 ${s.name}？`} onConfirm={() => control(s.name, 'restart')}>
                      <Button size="small">重启</Button>
                    </Popconfirm>
                  </Space>
                )
              }
            >
              <Row gutter={8}>
                <Col span={12}><Statistic title="运行时长" value={fmtUptime(s.uptimeSeconds)} valueStyle={{ fontSize: 16 }} /></Col>
                <Col span={12}><Statistic title="请求数" value={s.requestsTotal} valueStyle={{ fontSize: 16 }} precision={0} /></Col>
                <Col span={12}><Statistic title="P99 时延" value={s.p99BucketSeconds * 1000} suffix="ms" valueStyle={{ fontSize: 16 }} precision={0} /></Col>
                <Col span={12}><Statistic title="Goroutines" value={s.goroutines} valueStyle={{ fontSize: 16 }} precision={0} /></Col>
              </Row>
              <div style={{ marginTop: 8, color: '#8c6f5d', fontSize: 12 }}>
                {s.url}{s.downSince ? ` · 自 ${s.downSince.slice(11, 19)} 不可用` : ''}
              </div>
            </Card>
          </Col>
        ))}
      </Row>

      <Card size="small" title="告警（service_down）" style={{ marginTop: 16 }}>
        <Table
          size="small" rowKey="id" pagination={{ pageSize: 5 }} dataSource={alerts}
          columns={[
            { title: '服务', dataIndex: 'service', width: 120 },
            { title: '规则', dataIndex: 'rule', width: 130 },
            { title: '详情', dataIndex: 'detail' },
            { title: '开始', dataIndex: 'openedAt', width: 170, render: (v) => v?.slice(0, 19).replace('T', ' ') },
            { title: '状态', dataIndex: 'resolvedAt', width: 90, render: (v) => (v ? <Tag color="green">已恢复</Tag> : <Tag color="red">未恢复</Tag>) },
          ]}
        />
      </Card>

      <Card size="small" title="运维审计（最近 100 条）" style={{ marginTop: 16 }}>
        <Table
          size="small" rowKey="id" pagination={{ pageSize: 5 }} dataSource={audit}
          columns={[
            { title: '时间', dataIndex: 'createdAt', width: 170, render: (v) => v?.slice(0, 19).replace('T', ' ') },
            { title: '操作人', dataIndex: 'username', width: 110 },
            { title: '服务', dataIndex: 'service', width: 110 },
            { title: '动作', dataIndex: 'action', width: 90, render: (v) => <Tag>{v}</Tag> },
            { title: '结果', dataIndex: 'result', width: 90, render: (v) => (v === 'ok' ? <Tag color="green">成功</Tag> : <Tag color="red">失败</Tag>) },
            { title: '详情', dataIndex: 'detail' },
          ]}
        />
      </Card>
    </div>
  )
}
