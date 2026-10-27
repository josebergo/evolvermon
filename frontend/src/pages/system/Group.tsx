import { useEffect, useState } from 'react'
import { Button, Card, Table } from 'antd'
import { get } from '../../api/client'

// 集团视图（M5）：多厂关键指标横向对比
export default function Group() {
  const [rows, setRows] = useState<any[]>([])

  const reload = () => get('/group/summary').then(setRows).catch(() => {})
  useEffect(() => { reload() }, [])

  return (
    <Card title="集团视图（各厂在制/超期/今日产量/库存金额/待办单据）" extra={<Button onClick={reload}>刷新</Button>}>
      <Table rowKey="factoryId" size="middle" dataSource={rows} pagination={false}
        columns={[
          { title: '工厂', dataIndex: 'factory' },
          { title: '在制工单', dataIndex: 'woActive', width: 100 },
          { title: '超期工单', dataIndex: 'woOverdue', width: 100, render: (v) => (v > 0 ? <b style={{ color: '#B3251B' }}>{v}</b> : v) },
          { title: '今日合格产量', dataIndex: 'todayOk', width: 120 },
          { title: '库存金额(元)', dataIndex: 'stockValue', width: 130, render: (v) => <b>¥{Number(v).toFixed(2)}</b> },
          { title: '待执行单据', dataIndex: 'pendingDocs', width: 110 },
        ]} />
    </Card>
  )
}
