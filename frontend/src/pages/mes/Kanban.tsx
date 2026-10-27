import { useEffect, useState } from 'react'
import { Card, Col, Progress, Row, Space, Statistic, Table, Tag, Typography } from 'antd'
import { get } from '../../api/client'

// 车间看板（FR-MES-050/051）
export default function Kanban() {
  const [cards, setCards] = useState<any[]>([])
  const [today, setToday] = useState<any>(null)

  const reload = () => {
    get('/kanban/board').then(setCards)
    get('/kanban/today').then(setToday)
  }
  useEffect(() => {
    reload()
    const t = setInterval(reload, 15000) // 看板 15s 自动刷新
    return () => clearInterval(t)
  }, [])

  const stColor: Record<string, string> = { released: '#1677ff', running: '#52c41a', paused: '#fa8c16' }
  const stText: Record<string, string> = { released: '已下达', running: '执行中', paused: '暂停' }

  return (
    <div>
      {today && (
        <Row gutter={12} style={{ marginBottom: 12 }}>
          <Col span={6}><Card><Statistic title="今日合格产量" value={today.totalOk} suffix="件" valueStyle={{ color: '#3f8600' }} /></Card></Col>
          <Col span={6}><Card><Statistic title="今日不良" value={today.totalDefect} suffix="件" valueStyle={{ color: '#cf1322' }} /></Card></Col>
          <Col span={6}><Card><Statistic title="一次合格率" value={today.passRate} precision={1} suffix="%" /></Card></Col>
          <Col span={6}><Card><Statistic title="在制工单" value={cards.length} suffix="张" /></Card></Col>
        </Row>
      )}
      <Card title="在制工单墙（超期红牌 · 15 秒自动刷新）">
        {cards.length === 0 && <Typography.Text type="secondary">暂无在制工单</Typography.Text>}
        <Row gutter={[12, 12]}>
          {cards.map((c) => (
            <Col key={c.id} xs={24} sm={12} md={8} lg={6}>
              <Card size="small" style={{ borderLeft: `4px solid ${stColor[c.status] || '#999'}`, background: c.overdue ? '#fff1f0' : undefined }}>
                <Space direction="vertical" size={2} style={{ width: '100%' }}>
                  <Space style={{ justifyContent: 'space-between', width: '100%' }}>
                    <b>{c.docNo}</b>
                    {c.overdue ? <Tag color="red">超期</Tag> : <Tag color={stColor[c.status]}>{stText[c.status]}</Tag>}
                  </Space>
                  <Progress percent={Math.round(c.progress)} size="small" status={c.overdue ? 'exception' : 'active'} />
                  <span style={{ fontSize: 12, color: '#888' }}>合格 {c.goodQty} / 计划 {c.qty} · 完工 {c.planEndAt?.slice(0, 10)}</span>
                </Space>
              </Card>
            </Col>
          ))}
        </Row>
      </Card>
      {today && (
        <Card title="今日按工单产量" style={{ marginTop: 12 }} styles={{ body: { padding: 12 } }}>
          <Table rowKey="workOrderId" size="small" pagination={false} dataSource={today.rows || []}
            columns={[
              { title: '工单', dataIndex: 'docNo' },
              { title: '合格', dataIndex: 'qtyOk' },
              { title: '不良', dataIndex: 'qtyDefect' },
            ]} />
        </Card>
      )}
    </div>
  )
}
