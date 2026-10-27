import { useEffect, useState } from 'react'
import { Col, Progress, Row, Statistic } from 'antd'
import { get } from '../../api/client'

// 大屏看板（M3）：全屏轮播模式，投屏到车间电视
export default function Board() {
  const [cards, setCards] = useState<any[]>([])
  const [today, setToday] = useState<any>(null)
  const [board, setBoard] = useState<any[]>([])
  const [tick, setTick] = useState(0)

  useEffect(() => {
    Promise.all([get('/kanban/board'), get('/kanban/today'), get('/plan/dispatch-board')])
      .then(([k, t, b]: any) => { setCards(k || []); setToday(t); setBoard(b || []) })
      .catch(() => {})
    const t1 = setInterval(() => { setTick((x) => x + 1) }, 10000) // 10s 轮换面板
    const t2 = setInterval(() => {
      Promise.all([get('/kanban/board'), get('/kanban/today')])
        .then(([k, t]: any) => { setCards(k || []); setToday(t) }).catch(() => {})
    }, 15000)
    return () => { clearInterval(t1); clearInterval(t2) }
  }, [])

  const stColor: Record<string, string> = { released: '#F3A93B', running: '#7CC24E', paused: '#E07B39' }
  const view = tick % 3

  return (
    <div style={{ background: '#241205', color: '#FFF3E4', minHeight: '100vh', padding: 20 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 12 }}>
        <h2 style={{ color: '#F3A93B', margin: 0 }}>EvolverMon 车间数字大屏</h2>
        <span style={{ fontSize: 18 }}>{today ? `今日合格 ${today.totalOk} 件 · 合格率 ${Number(today.passRate).toFixed(1)}%` : ''}</span>
      </div>

      {view === 0 && (
        <Row gutter={[12, 12]}>
          {cards.map((c) => (
            <Col key={c.id} xs={24} sm={12} md={6}>
              <div style={{ border: '1px solid #5C3A1A', borderRadius: 8, padding: 14, background: c.overdue ? '#4A1408' : '#3A2410' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                  <b style={{ fontSize: 17 }}>{c.docNo}</b>
                  {c.overdue ? <span style={{ color: '#FF9A7A' }}>超期</span> : <span style={{ color: stColor[c.status] }}>{c.status}</span>}
                </div>
                <Progress percent={Math.round(c.progress)} strokeColor={c.overdue ? '#FF7A50' : '#7CC24E'} />
                <div style={{ color: '#E8C9A8' }}>合格 {c.goodQty} / 计划 {c.qty} · 完工 {c.planEndAt?.slice(0, 10)}</div>
              </div>
            </Col>
          ))}
          {cards.length === 0 && <Col span={24} style={{ textAlign: 'center', padding: 80, color: '#B08A62' }}>暂无在制工单</Col>}
        </Row>
      )}

      {view === 1 && today && (
        <Row gutter={12}>
          <Col span={6}><div style={{ border: '1px solid #5C3A1A', borderRadius: 8, padding: 20, background: '#3A2410' }}><Statistic title="今日合格" value={today.totalOk} valueStyle={{ color: '#7CC24E', fontSize: 40 }} /></div></Col>
          <Col span={6}><div style={{ border: '1px solid #5C3A1A', borderRadius: 8, padding: 20, background: '#3A2410' }}><Statistic title="今日不良" value={today.totalDefect} valueStyle={{ color: '#FF9A7A', fontSize: 40 }} /></div></Col>
          <Col span={6}><div style={{ border: '1px solid #5C3A1A', borderRadius: 8, padding: 20, background: '#3A2410' }}><Statistic title="一次合格率" value={Number(today.passRate).toFixed(1)} suffix="%" valueStyle={{ color: '#F3A93B', fontSize: 40 }} /></div></Col>
          <Col span={6}><div style={{ border: '1px solid #5C3A1A', borderRadius: 8, padding: 20, background: '#3A2410' }}><Statistic title="在制工单" value={cards.length} valueStyle={{ color: '#FFF3E4', fontSize: 40 }} /></div></Col>
          <Col span={24} style={{ marginTop: 12 }}>
            {(today.rows || []).map((r: any, i: number) => (
              <div key={i} style={{ fontSize: 20, padding: 8, borderBottom: '1px solid #5C3A1A' }}>
                {r.docNo} —— 合格 <b style={{ color: '#7CC24E' }}>{r.qtyOk}</b> · 不良 <b style={{ color: '#FF9A7A' }}>{r.qtyDefect}</b>
              </div>
            ))}
          </Col>
        </Row>
      )}

      {view === 2 && (
        <Row gutter={[12, 12]}>
          {board.map((b) => (
            <Col key={b.workCenterId} xs={24} sm={12} md={6}>
              <div style={{ border: '1px solid #5C3A1A', borderRadius: 8, padding: 16, background: '#3A2410' }}>
                <b style={{ fontSize: 18 }}>{b.workCenter}</b>
                <div style={{ color: '#E8C9A8', marginTop: 8 }}>待派工 {b.waiting}</div>
                <div style={{ color: '#F3A93B' }}>已派工 {b.assigned}</div>
                <div style={{ color: '#7CC24E' }}>执行中 {b.running}</div>
              </div>
            </Col>
          ))}
        </Row>
      )}
      <div style={{ textAlign: 'center', color: '#B08A62', marginTop: 12 }}>面板每 10 秒轮换 · 数据每 15 秒刷新 · 浏览器返回键退出</div>
    </div>
  )
}
