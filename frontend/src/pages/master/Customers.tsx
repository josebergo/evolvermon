import { Form, Input, InputNumber } from 'antd'
import CrudTable from '../../components/CrudTable'

export default function Customers() {
  return (
    <CrudTable
      title="客户管理"
      api="/customers"
      searchPlaceholder="编码/名称"
      columns={[
        { title: '编码', dataIndex: 'code', width: 100 },
        { title: '名称', dataIndex: 'name' },
        { title: '联系人', dataIndex: 'contact', width: 110 },
        { title: '电话', dataIndex: 'phone', width: 130 },
        { title: '税号', dataIndex: 'taxNo', width: 160 },
        { title: '信用额度', dataIndex: 'creditLimit', width: 110 },
        { title: '状态', dataIndex: 'status', width: 70, render: (v) => (v === 1 ? '启用' : '停用') },
      ]}
      formItems={() => (
        <>
          <Form.Item name="code" label="客户编码" rules={[{ required: true }]}><Input placeholder="C0002" /></Form.Item>
          <Form.Item name="name" label="客户名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="contact" label="联系人"><Input /></Form.Item>
          <Form.Item name="phone" label="电话"><Input /></Form.Item>
          <Form.Item name="taxNo" label="税号"><Input /></Form.Item>
          <Form.Item name="creditLimit" label="信用额度" initialValue={0}><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="status" label="状态" initialValue={1}><InputNumber min={0} max={1} style={{ width: '100%' }} /></Form.Item>
        </>
      )}
    />
  )
}
