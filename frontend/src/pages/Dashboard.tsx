import { useEffect, useState } from 'react'
import { Badge, Card, Col, List, Row, Statistic, Table, Tag, Typography } from 'antd'
import { Area, AreaChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { get } from '../api/client'
import { useAuth } from '../stores/auth'

export default function Dashboard() {
  const [data, setData] = useState<any>(null)
  const [insights, setInsights] = useState<any[]>([])
  const { user, factoryName } = useAuth()

  useEffect(() => {
    get('/dashboard/summary').then(setData).catch(() => {})
    get('/advisor/insights').then((d: any) => setInsights(d.insights || [])).catch(() => {})
  }, [])

  const c = data?.counts || {}
  const trend = (data?.reportTrend || []).map((t: any) => ({ day: t.day, qty: Number(t.qtyOk) }))

  return (
    <div>
      <Typography.Title level={4} style={{ marginTop: 0 }}>
        你好，{user?.realName || user?.username} <Typography.Text type="secondary" style={{ fontSize: 14 }}>当前工厂：{factoryName}</Typography.Text>
      </Typography.Title>
      <Row gutter={[12, 12]}>
        <Col xs={12} md={6}><Card size="small"><Statistic title="待审核销售订单" value={c.salesPending ?? '-'} suffix="张" /></Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="在制工单" value={c.woActive ?? '-'} suffix="张" valueStyle={{ color: '#1677ff' }} /></Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="超期工单" value={c.woOverdue ?? '-'} suffix="张" valueStyle={{ color: c.woOverdue > 0 ? '#cf1322' : undefined }} /></Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="到货中采购单" value={c.poArriving ?? '-'} suffix="张" /></Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="待执行入库单" value={c.rcvTodo ?? '-'} suffix="张" valueStyle={{ color: '#3f8600' }} /></Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="待执行出库单" value={c.shpTodo ?? '-'} suffix="张" valueStyle={{ color: '#1677ff' }} /></Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="待检质检单" value={c.qcPending ?? '-'} suffix="张" valueStyle={{ color: '#fa8c16' }} /></Card></Col>
        <Col xs={12} md={6}><Card size="small"><Statistic title="物料主数据" value={c.materialCount ?? '-'} suffix="项" /></Card></Col>
      </Row>
      <Row gutter={12} style={{ marginTop: 12 }}>
        <Col xs={24} lg={14}>
          <Card title="近 7 日报工产量趋势" size="small">
            <ResponsiveContainer width="100%" height={220}>
              <AreaChart data={trend}>
                <XAxis dataKey="day" /><YAxis />
                <Tooltip />
                <Area type="monotone" dataKey="qty" stroke="#1677ff" fill="#1677ff33" name="合格产量" />
              </AreaChart>
            </ResponsiveContainer>
          </Card>
        </Col>
        <Col xs={24} lg={10}>
          <Card title="安全库存预警" size="small">
            {(data?.stockAlerts || []).length === 0 && <Typography.Text type="secondary">暂无预警</Typography.Text>}
            <List size="small" dataSource={data?.stockAlerts || []}
              renderItem={(a: any) => (
                <List.Item>
                  <Badge status="error" text={<>{a.code} {a.name}：现存量 {a.onHand} &lt; 安全库存 {a.safety}</>} />
                </List.Item>
              )} />
          </Card>
        </Col>
      </Row>
      <Row gutter={12} style={{ marginTop: 12 }}>
        <Col span={24}>
          <Card title="智能建议（启发式：缺料 / 交期 / 产能）" size="small">
            {insights.length === 0 && <Typography.Text type="secondary">暂无风险洞察 —— 一切正常</Typography.Text>}
            {insights.map((it: any, i: number) => (
              <div key={i} style={{ padding: '8px 0', borderBottom: '1px solid #E8D5C0' }}>
                <Tag color={it.level === 'high' ? 'error' : 'warning'}>{it.kind}</Tag>
                <b>{it.title}</b>
                <span style={{ color: '#5C4433', marginLeft: 12 }}>{it.detail}</span>
                <div style={{ color: '#A34713', marginTop: 2 }}>→ {it.action}</div>
              </div>
            ))}
          </Card>
        </Col>
      </Row>
    </div>
  )
}
