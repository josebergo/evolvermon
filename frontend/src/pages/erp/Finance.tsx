import { useEffect, useState } from 'react'
import { Button, Card, DatePicker, Space, Table, Tag } from 'antd'
import { get } from '../../api/client'

// 财务凭证导出（M4）：应收/应付凭证 → 对接用友/金蝶
export default function Finance() {
  const [rows, setRows] = useState<any[]>([])
  const [range, setRange] = useState<[any, any] | null>(null)

  const reload = async () => {
    const params: any = {}
    if (range?.[0]) params.from = range[0].format('YYYY-MM-DD')
    if (range?.[1]) params.to = range[1].format('YYYY-MM-DD')
    const d: any = await get('/finance/vouchers', params)
    setRows(d || [])
  }
  useEffect(() => { reload() }, [])

  const ar = rows.filter((r) => r.type === 'AR').reduce((s, r) => s + r.amount, 0)
  const ap = rows.filter((r) => r.type === 'AP').reduce((s, r) => s + r.amount, 0)

  return (
    <Card title="财务凭证导出（应收/应付 · 可导出 CSV 对接用友/金蝶）" extra={<Space>
      <DatePicker.RangePicker onChange={(v) => setRange(v)} />
      <Button onClick={reload}>查询</Button>
      <Button onClick={() => {
        const p = new URLSearchParams()
        if (range?.[0]) p.set('from', range[0].format('YYYY-MM-DD'))
        if (range?.[1]) p.set('to', range[1].format('YYYY-MM-DD'))
        p.set('format', 'csv')
        window.open(`/api/v1/finance/vouchers?${p}&access_token=${localStorage.getItem('em_at')}`, '_blank')
      }}>导出 CSV</Button>
    </Space>}>
      <Space style={{ marginBottom: 12 }}>
        <Tag color="green" style={{ fontSize: 14 }}>应收合计 ¥{ar.toFixed(2)}</Tag>
        <Tag color="orange" style={{ fontSize: 14 }}>应付合计 ¥{ap.toFixed(2)}</Tag>
      </Space>
      <Table rowKey={(r: any) => r.voucherNo} size="small" dataSource={rows} pagination={{ pageSize: 15 }}
        columns={[
          { title: '凭证号', dataIndex: 'voucherNo', width: 130 },
          { title: '类型', dataIndex: 'type', width: 70, render: (v) => <Tag color={v === 'AR' ? 'green' : 'orange'}>{v === 'AR' ? '应收' : '应付'}</Tag> },
          { title: '业务单号', dataIndex: 'docNo', width: 150 },
          { title: '往来单位', dataIndex: 'partner' },
          { title: '金额', dataIndex: 'amount', width: 110, render: (v) => <b>¥{v}</b> },
          { title: '借方科目', dataIndex: 'debitAcc', width: 140 },
          { title: '贷方科目', dataIndex: 'creditAcc', width: 140 },
        ]} />
    </Card>
  )
}
