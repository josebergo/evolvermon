import { Form, Input, InputNumber, Select } from 'antd'
import CrudTable from '../../components/CrudTable'

const types = [{ value: 'labor', label: '人工' }, { value: 'machine', label: '设备' }, { value: 'line', label: '产线' }]

export default function WorkCenters() {
  return (
    <CrudTable
      title="工作中心"
      api="/work-centers"
      searchPlaceholder="编码/名称"
      columns={[
        { title: '编码', dataIndex: 'code', width: 110 },
        { title: '名称', dataIndex: 'name' },
        { title: '类型', dataIndex: 'type', width: 90, render: (v) => types.find((t) => t.value === v)?.label || v },
        { title: '日产能', dataIndex: 'capacityPerDay', width: 90 },
        { title: '费率(元/时)', dataIndex: 'costRate', width: 100 },
        { title: '状态', dataIndex: 'status', width: 70, render: (v) => (v === 1 ? '启用' : '停用') },
      ]}
      formItems={() => (
        <>
          <Form.Item name="code" label="编码" rules={[{ required: true }]}><Input placeholder="WC-XXX" /></Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="type" label="类型" initialValue="machine"><Select options={types} /></Form.Item>
          <Form.Item name="capacityPerDay" label="日产能" initialValue={0}><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="costRate" label="费率(元/时)" initialValue={0}><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="status" label="状态" initialValue={1}><Select options={[{ value: 1, label: '启用' }, { value: 0, label: '停用' }]} /></Form.Item>
        </>
      )}
    />
  )
}
