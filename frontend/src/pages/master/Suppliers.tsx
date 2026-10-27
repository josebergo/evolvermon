import { Form, Input, InputNumber } from 'antd'
import CrudTable from '../../components/CrudTable'

export default function Suppliers() {
  return (
    <CrudTable
      title="供应商管理"
      api="/suppliers"
      searchPlaceholder="编码/名称"
      columns={[
        { title: '编码', dataIndex: 'code', width: 100 },
        { title: '名称', dataIndex: 'name' },
        { title: '联系人', dataIndex: 'contact', width: 110 },
        { title: '电话', dataIndex: 'phone', width: 130 },
        { title: '交期(天)', dataIndex: 'leadTimeDays', width: 90 },
        { title: '付款条件', dataIndex: 'paymentTerm' },
        { title: '状态', dataIndex: 'status', width: 70, render: (v) => (v === 1 ? '启用' : '停用') },
      ]}
      formItems={() => (
        <>
          <Form.Item name="code" label="供应商编码" rules={[{ required: true }]}><Input placeholder="S0002" /></Form.Item>
          <Form.Item name="name" label="供应商名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="contact" label="联系人"><Input /></Form.Item>
          <Form.Item name="phone" label="电话"><Input /></Form.Item>
          <Form.Item name="leadTimeDays" label="交期(天)" initialValue={7}><InputNumber min={1} style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="paymentTerm" label="付款条件"><Input placeholder="月结 30 天" /></Form.Item>
        </>
      )}
    />
  )
}
