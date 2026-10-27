import { useEffect, useState } from 'react'
import { Button, Card, Input, Space, Table, message } from 'antd'
import { get, post } from '../../api/client'

export default function Params() {
  const [rows, setRows] = useState<any[]>([])
  const [editing, setEditing] = useState<any>(null)

  const reload = async () => setRows(await get('/params'))
  useEffect(() => { reload() }, [])

  return (
    <Card title="系统参数（租户级）" extra={<Button onClick={reload}>刷新</Button>}>
      <Table rowKey="id" size="middle" dataSource={rows} pagination={false}
        columns={[
          { title: '参数键', dataIndex: 'key', width: 220 },
          { title: '值', dataIndex: 'value', width: 200, render: (v, r) => editing?.id === r.id ? (
            <Input size="small" value={editing.value} onChange={(e) => setEditing({ ...editing, value: e.target.value })} />
          ) : String(v) },
          { title: '说明', dataIndex: 'remark' },
          { title: '更新时间', dataIndex: 'updatedAt', width: 160, render: (v) => v?.slice(0, 19).replace('T', ' ') },
          {
            title: '操作', width: 120, render: (_, r) => editing?.id === r.id ? (
              <Space>
                <a onClick={async () => { await post('/params', { key: r.key, value: editing.value, remark: r.remark }); message.success('已保存'); setEditing(null); reload() }}>保存</a>
                <a onClick={() => setEditing(null)}>取消</a>
              </Space>
            ) : <a onClick={() => setEditing({ ...r, value: String(r.value).replace(/^"|"$/g, '') })}>修改</a>,
          },
        ]} />
    </Card>
  )
}
