import { useEffect, useState } from 'react'
import { Button, Card, message, Space, Spin, Tag, Typography } from 'antd'
import { get, post } from '../../api/client'

// 产能排产（M3）：一键有限产能前推排程 + 工作中心甘特视图
export default function Scheduling() {
  const [groups, setGroups] = useState<any[]>([])
  const [loading, setLoading] = useState(false)
  const [scheduling, setScheduling] = useState(false)
  const [board, setBoard] = useState<any[]>([])

  const reload = () => {
    setLoading(true)
    Promise.all([get('/plan/gantt'), get('/plan/dispatch-board')])
      .then(([g, b]: any) => { setGroups(g || []); setBoard(b || []) })
      .finally(() => setLoading(false))
  }
  useEffect(() => { reload() }, [])

  const schedule = async () => {
    setScheduling(true)
    try {
      const r: any = await post('/plan/schedule')
      message.success(`已排产 ${r.scheduled} 张工单`)
      reload()
    } finally { setScheduling(false) }
  }

  return (
    <div>
      <Card title="产能排产（按优先级+交期排序，工作中心日产能约束，前推排程）" extra={<Space>
        <Button onClick={reload}>刷新</Button>
        <Button type="primary" loading={scheduling} onClick={schedule}>一键排产</Button>
      </Space>}>
        <Space wrap size={16} style={{ marginBottom: 12 }}>
          {board.map((b) => (
            <Tag key={b.workCenterId} style={{ fontSize: 13, padding: '4px 10px' }}>
              {b.workCenter}：待派 {b.waiting} · 已派 {b.assigned} · 执行中 {b.running}
            </Tag>
          ))}
        </Space>
        {loading ? <Spin /> : (
          groups.map((g) => (
            <div key={g.workCenterId} style={{ marginBottom: 16 }}>
              <Typography.Text strong>{g.workCenter}</Typography.Text>
              <Typography.Text type="secondary" style={{ marginLeft: 8 }}>日产能 {g.capacityPerDay}</Typography.Text>
              <div style={{ marginTop: 6 }}>
                {(g.bars || []).length === 0 && <Typography.Text type="secondary">暂无排产（先「一键排产」）</Typography.Text>}
                {(g.bars || []).map((b: any, i: number) => (
                  <div key={i} style={{
                    display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4,
                    background: b.overdue ? '#fff1f0' : undefined, padding: '2px 6px', borderRadius: 4,
                  }}>
                    <Tag color={b.priority <= 3 ? 'red' : b.priority >= 7 ? 'default' : 'blue'}>P{b.priority}</Tag>
                    <span style={{ width: 140 }}>{b.woNo}</span>
                    <span style={{ width: 90 }}>{b.seq}. {b.name}</span>
                    <b style={{ width: 110 }}>{b.start}</b>→<b style={{ width: 110 }}>{b.end}</b>
                    {b.overdue && <Tag color="red">超期风险</Tag>}
                  </div>
                ))}
              </div>
            </div>
          ))
        )}
      </Card>
    </div>
  )
}
