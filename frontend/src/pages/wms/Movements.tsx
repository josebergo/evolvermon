import { useEffect, useState } from 'react'
import { Button, Card, Input, Space, Table, Tag } from 'antd'
import { get } from '../../api/client'
import CrudTable from '../../components/CrudTable'

export default function Movements() {
  return (
    <CrudTable
      title="库存流水（只增不改，账实核对以流水为准）"
      api="/stock-movements"
      searchPlaceholder="批次号"
      hideAdd
      hideDelete
      columns={[
        { title: '时间', dataIndex: 'createdAt', width: 170, render: (v) => v?.slice(0, 19).replace('T', ' ') },
        { title: '方向', dataIndex: 'direction', width: 70, render: (v) => <Tag color={v === 'IN' ? 'green' : 'red'}>{v === 'IN' ? '入库' : '出库'}</Tag> },
        { title: '物料', dataIndex: 'materialId' },
        { title: '批次', dataIndex: 'batchNo', width: 110 },
        { title: '数量', dataIndex: 'qty', width: 90 },
        { title: '业务', dataIndex: 'bizType', width: 100 },
        { title: '来源单号', dataIndex: 'sourceDocNo', width: 140 },
        { title: '变动前', dataIndex: 'beforeQty', width: 80 },
        { title: '变动后', dataIndex: 'afterQty', width: 80 },
      ]}
    />
  )
}
