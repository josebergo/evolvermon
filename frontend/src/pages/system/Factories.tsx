import { Form, Input, Select } from 'antd'
import CrudTable from '../../components/CrudTable'

export default function Factories() {
  return (
    <CrudTable
      title="工厂列表（多工厂部署，切换工厂见右上角）"
      api="/factories"
      searchPlaceholder="工厂编码/名称"
      columns={[
        { title: '编码', dataIndex: 'code', width: 100 },
        { title: '名称', dataIndex: 'name' },
        { title: '地址', dataIndex: 'address' },
        { title: '负责人', dataIndex: 'managerUser', width: 120 },
        { title: '状态', dataIndex: 'status', width: 80, render: (v) => (v === 1 ? '启用' : '停用') },
        { title: '创建时间', dataIndex: 'createdAt', width: 160, render: (v) => v?.slice(0, 19).replace('T', ' ') },
      ]}
      formItems={() => (
        <>
          <Form.Item name="code" label="工厂编码" rules={[{ required: true }]}><Input placeholder="如 DG" /></Form.Item>
          <Form.Item name="name" label="工厂名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="address" label="地址"><Input /></Form.Item>
          <Form.Item name="managerUser" label="负责人"><Input /></Form.Item>
          <Form.Item name="status" label="状态" initialValue={1}>
            <Select options={[{ value: 1, label: '启用' }, { value: 0, label: '停用' }]} />
          </Form.Item>
        </>
      )}
    />
  )
}
