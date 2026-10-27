import { Form, Input, InputNumber, Modal, Select } from 'antd'
import CrudTable from '../../components/CrudTable'
import { get } from '../../api/client'

const categories = [
  { value: 'raw', label: '原料' }, { value: 'semi', label: '半成品' },
  { value: 'finished', label: '成品' }, { value: 'aux', label: '辅料' }, { value: 'package', label: '包材' },
]
const sources = [
  { value: 'buy', label: '外购' }, { value: 'make', label: '自制' }, { value: 'outsource', label: '委外' },
]

export default function Materials() {
  return (
    <CrudTable
      title="物料主数据"
      api="/materials"
      searchPlaceholder="编码/名称/规格"
      columns={[
        { title: '编码', dataIndex: 'code', width: 110 },
        { title: '名称', dataIndex: 'name' },
        { title: '规格', dataIndex: 'spec' },
        { title: '类别', dataIndex: 'category', width: 90, render: (v) => categories.find((c) => c.value === v)?.label || v },
        { title: '单位', dataIndex: 'unit', width: 60 },
        { title: '来源', dataIndex: 'sourceType', width: 80, render: (v) => sources.find((s) => s.value === v)?.label || v },
        { title: '批次管理', dataIndex: 'batchManaged', width: 90, render: (v) => (v ? '是' : '否') },
        { title: '安全库存', dataIndex: 'safetyStock', width: 90 },
        { title: '单价', dataIndex: 'price', width: 80 },
        { title: '提前期(天)', dataIndex: 'leadTimeDays', width: 95 },
        { title: '状态', dataIndex: 'status', width: 70, render: (v) => (v === 1 ? '启用' : '停用') },
      ]}
      rowActions={(row) => (
        <a key="wu" onClick={async () => {
          const data: any = await get(`/materials/${row.id}/where-used`)
          Modal.info({
            title: `「哪里用到」- ${row.code}`,
            width: 640,
            content: (
              <div>
                {(data || []).length === 0 && <div>未被任何 BOM 引用</div>}
                {(data || []).map((r: any, i: number) => (
                  <div key={i}>父项 {r.parentCode}（{r.parentName}）· 版本 {r.versionNo} · [{r.status}] · 单耗 {r.qty}</div>
                ))}
              </div>
            ),
          })
        }}>哪里用到</a>
      )}
      formItems={() => (
        <>
          <Form.Item name="code" label="物料编码" rules={[{ required: true }]}><Input placeholder="如 RM-3005" /></Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="spec" label="规格型号"><Input /></Form.Item>
          <Form.Item name="category" label="类别" rules={[{ required: true }]} initialValue="raw"><Select options={categories} /></Form.Item>
          <Form.Item name="unit" label="单位" rules={[{ required: true }]} initialValue="个"><Input /></Form.Item>
          <Form.Item name="sourceType" label="来源类型" initialValue="buy"><Select options={sources} /></Form.Item>
          <Form.Item name="batchManaged" label="批次管理" valuePropName="checked"><Select options={[{ value: true, label: '是' }, { value: false, label: '否' }]} /></Form.Item>
          <Form.Item name="serialManaged" label="序列号管理" valuePropName="checked"><Select options={[{ value: true, label: '是' }, { value: false, label: '否' }]} /></Form.Item>
          <Form.Item name="safetyStock" label="安全库存" initialValue={0}><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="price" label="参考单价" initialValue={0}><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="leadTimeDays" label="提前期(天)" initialValue={0}><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="status" label="状态" initialValue={1}><Select options={[{ value: 1, label: '启用' }, { value: 0, label: '停用' }]} /></Form.Item>
        </>
      )}
    />
  )
}
