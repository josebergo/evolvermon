import CrudTable from '../../components/CrudTable'

export default function AuditLogs() {
  return (
    <CrudTable
      title="审计日志（只读，写操作自动记录）"
      api="/audit-logs"
      searchPlaceholder="操作人"
      hideAdd
      hideDelete
      columns={[
        { title: '时间', dataIndex: 'createdAt', width: 170, render: (v) => v?.slice(0, 19).replace('T', ' ') },
        { title: '操作人', dataIndex: 'username', width: 110 },
        { title: '动作', dataIndex: 'action', width: 120 },
        { title: '对象类型', dataIndex: 'objectType', width: 120 },
        { title: '对象', dataIndex: 'objectDesc', width: 180 },
        { title: '详情', dataIndex: 'after', render: (v) => (v ? String(v).slice(0, 80) : '') },
        { title: 'IP', dataIndex: 'ip', width: 120 },
      ]}
    />
  )
}
