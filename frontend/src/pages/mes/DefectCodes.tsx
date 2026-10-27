import { Form, Input, InputNumber } from 'antd'
import CrudTable from '../../components/CrudTable'

export default function DefectCodes() {
  return (
    <CrudTable
      title="不良代码字典"
      api="/defect-codes"
      searchPlaceholder="编码/名称"
      columns={[
        { title: '编码', dataIndex: 'code', width: 100 },
        { title: '名称', dataIndex: 'name' },
        { title: '分类', dataIndex: 'category', width: 100 },
        { title: '排序', dataIndex: 'sort', width: 70 },
        { title: '状态', dataIndex: 'status', width: 70, render: (v) => (v === 1 ? '启用' : '停用') },
      ]}
      formItems={() => (
        <>
          <Form.Item name="code" label="编码" rules={[{ required: true }]}><Input placeholder="D006" /></Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="category" label="分类" initialValue="外观"><Input /></Form.Item>
          <Form.Item name="sort" label="排序" initialValue={99}><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="status" label="状态" initialValue={1}><InputNumber min={0} max={1} style={{ width: '100%' }} /></Form.Item>
        </>
      )}
    />
  )
}
