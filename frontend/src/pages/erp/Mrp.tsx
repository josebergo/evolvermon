import { useState } from 'react'
import { Button, Card, Space, Table, Tag, Typography, message } from 'antd'
import { get, post } from '../../api/client'

// MRP 运算（FR-ERP-070~077）
export default function Mrp() {
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<any>(null)
  const [selected, setSelected] = useState<any[]>([])

  const run = async () => {
    setRunning(true)
    try {
      const data: any = await post('/mrp/run', {})
      setResult(data)
      message.success(`运算完成：${data.suggestions?.length || 0} 条建议`)
    } finally { setRunning(false) }
  }

  const convert = async (to: 'wo' | 'pr') => {
    if (selected.length === 0) { message.warning('请勾选建议行'); return }
    const res: any = await post('/mrp/suggestions/convert', { suggestions: selected, to })
    const txt = to === 'wo' ? `工单：${res.workOrders?.join('、') || '无'}` : `请购单：${res.purchaseRequests?.join('、') || '无'}`
    message.success(`已转换 → ${txt}`)
    run()
  }

  return (
    <Card title="MRP 物料需求运算（净需求 = 毛需求 − 可用 − 在途 − 在制，按 BOM 逐层展开）" extra={
      <Space>
        <Button type="primary" loading={running} onClick={run}>运行 MRP</Button>
        <Button onClick={() => convert('wo')}>选中转工单</Button>
        <Button onClick={() => convert('pr')}>选中转请购</Button>
      </Space>
    }>
      {!result && <Typography.Text type="secondary">点击「运行 MRP」开始运算。数据源：已审核销售订单 + 当前库存/预占 + 在途采购 + 在制工单。</Typography.Text>}
      {result && (
        <>
          <Space style={{ marginBottom: 12 }}>
            <Tag color="blue">参与订单 {result.salesCount}</Tag>
            <Tag color="green">建议 {result.suggestions?.length || 0} 条</Tag>
          </Space>
          <Table rowKey={(r: any) => `${r.materialId}-${r.sourceType}`} size="small"
            dataSource={result.suggestions || []}
            rowSelection={{ onChange: (_: any[], rows: any[]) => setSelected(rows) }}
            pagination={false}
            columns={[
              { title: '物料', dataIndex: 'materialName' },
              { title: '建议类型', dataIndex: 'sourceType', width: 100, render: (v) => <Tag color={v === 'make' ? 'blue' : 'purple'}>{v === 'make' ? '生产' : '采购'}</Tag> },
              { title: '净需求量', dataIndex: 'netQty', width: 110 },
              { title: '建议日期', dataIndex: 'dueDate', width: 120, render: (v) => v?.slice(0, 10) },
              { title: '需求来源', dataIndex: 'sources', render: (v: string[]) => (v || []).join('；') },
            ]} />
        </>
      )}
    </Card>
  )
}
