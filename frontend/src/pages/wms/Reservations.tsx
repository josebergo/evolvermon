import CrudTable from '../../components/CrudTable'

export default function Reservations() {
  return (
    <CrudTable
      title="库存预占（销售审核/工单占用 → 出库核销）"
      api="/reservations"
      searchPlaceholder="来源单号"
      hideAdd
      statusOptions={[
        { value: 'held', label: '占用中' }, { value: 'consumed', label: '已核销' }, { value: 'released', label: '已释放' },
      ]}
      rowActions={(row, reload) => row.status === 'held' ? (
        <a key="rel" onClick={async () => {
          await import('../../api/client').then((m) => m.post(`/reservations/${row.id}/release`))
          import('antd').then(({ message }) => message.success('已释放'))
          reload()
        }}>释放</a>
      ) : null}
      columns={[
        { title: '业务', dataIndex: 'bizType', width: 80, render: (v) => (v === 'sales' ? '销售' : '工单') },
        { title: '来源单据ID', dataIndex: 'sourceDocId', width: 100 },
        { title: '数量', dataIndex: 'qty', width: 90 },
        { title: '状态', dataIndex: 'status', width: 90, render: (v) => ({ held: '占用中', consumed: '已核销', released: '已释放' } as any)[v] || v },
        { title: '创建时间', dataIndex: 'createdAt', width: 160, render: (v) => v?.slice(0, 19).replace('T', ' ') },
      ]}
    />
  )
}
