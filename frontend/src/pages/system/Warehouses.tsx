import { Form, Input, Select } from 'antd'
import CrudTable from '../../components/CrudTable'

const whTypes = [
  { value: 'raw', label: '原料仓' }, { value: 'finished', label: '成品仓' },
  { value: 'semi', label: '半成品仓' }, { value: 'defective', label: '不良品仓' }, { value: 'transit', label: '在途仓' },
]

export default function Warehouses() {
  return (
    <CrudTable
      title="仓库管理（挂在当前工厂下）"
      api="/warehouses"
      searchPlaceholder="仓库编码/名称"
      statusOptions={whTypes.map((t) => ({ value: t.value, label: t.label }))}
      columns={[
        { title: '编码', dataIndex: 'code', width: 120 },
        { title: '名称', dataIndex: 'name' },
        { title: '类型', dataIndex: 'type', width: 110, render: (v) => whTypes.find((t) => t.value === v)?.label || v },
        { title: '状态', dataIndex: 'status', width: 80, render: (v) => (v === 1 ? '启用' : '停用') },
      ]}
      formItems={() => (
        <>
          <Form.Item name="code" label="仓库编码" rules={[{ required: true }]}><Input placeholder="如 DG-RAW2" /></Form.Item>
          <Form.Item name="name" label="仓库名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="type" label="类型" rules={[{ required: true }]} initialValue="raw"><Select options={whTypes} /></Form.Item>
          <Form.Item name="status" label="状态" initialValue={1}><Select options={[{ value: 1, label: '启用' }, { value: 0, label: '停用' }]} /></Form.Item>
        </>
      )}
    />
  )
}
