import { useEffect, useState } from 'react'
import { Button, Card, Form, Input, InputNumber, Modal, Select, Space, Table, message } from 'antd'
import { get, post, put } from '../../api/client'

// 工艺路线：工序序列编辑（FR-ERP-031~032）
export default function Routings() {
  const [rows, setRows] = useState<any[]>([])
  const [mats, setMats] = useState<any[]>([])
  const [wcs, setWcs] = useState<any[]>([])
  const [editing, setEditing] = useState<any>(null)
  const [form] = Form.useForm()

  const reload = async () => setRows((await get('/routings', { page_size: 100 }))?.list || [])
  useEffect(() => {
    reload()
    get('/materials', { page_size: 200 }).then((d: any) => setMats(d.list || []))
    get('/work-centers', { page_size: 100 }).then((d: any) => setWcs(d.list || []))
  }, [])

  const matName = (id: number) => {
    const m = mats.find((x) => x.id === id)
    return m ? `${m.code} ${m.name}` : id
  }
  const wcName = (id: number) => wcs.find((w) => w.id === id)?.name || id

  const openEdit = (r?: any) => {
    const data = r ? { id: r.id, materialId: r.materialId, versionNo: r.versionNo, ops: r.ops || [] }
      : { materialId: undefined, versionNo: 'A', ops: [{}] }
    setEditing(data)
    form.setFieldsValue(data)
  }

  const save = async () => {
    const v = await form.validateFields()
    const payload = { materialId: Number(v.materialId), versionNo: v.versionNo, ops: v.ops }
    if (editing.id) await put(`/routings/${editing.id}`, { ...editing, ...payload })
    else await post('/routings', payload)
    setEditing(null); reload()
  }

  return (
    <Card title="工艺路线（工序序列，工单下达时固化快照）" extra={
      <Space><Button onClick={reload}>刷新</Button><Button type="primary" onClick={() => openEdit()}>新增工艺</Button></Space>
    }>
      <Table rowKey="id" size="middle" dataSource={rows} pagination={{ pageSize: 20 }}
        columns={[
          { title: 'ID', dataIndex: 'id', width: 60 },
          { title: '物料', dataIndex: 'materialId', render: (v) => matName(Number(v)) },
          { title: '版本', dataIndex: 'versionNo', width: 80 },
          { title: '工序', dataIndex: 'ops', render: (ops: any[]) => (ops || []).map((o) => `${o.seq}.${o.name}`).join(' → ') },
          { title: '操作', width: 100, render: (_, r) => <a onClick={() => openEdit(r)}>编辑</a> },
        ]} />
      <Modal title={editing?.id ? '编辑工艺路线' : '新增工艺路线'} open={!!editing} onOk={save}
        onCancel={() => setEditing(null)} width={820} destroyOnClose>
        <Form form={form} layout="vertical">
          <Space size={16}>
            <Form.Item name="materialId" label="物料" rules={[{ required: true }]} style={{ width: 320 }}>
              <Select showSearch optionFilterProp="label" options={mats.map((m) => ({ value: m.id, label: `${m.code} ${m.name}` }))} />
            </Form.Item>
            <Form.Item name="versionNo" label="版本号" rules={[{ required: true }]} style={{ width: 120 }}><Input /></Form.Item>
          </Space>
          <Form.List name="ops">
            {(fields, { add, remove }) => (
              <>
                {fields.map((f) => (
                  <Space key={f.key} align="baseline" style={{ display: 'flex', marginBottom: 4 }}>
                    <Form.Item name={[f.name, 'seq']} rules={[{ required: true }]} noStyle>
                      <InputNumber placeholder="序号" min={10} step={10} style={{ width: 80 }} />
                    </Form.Item>
                    <Form.Item name={[f.name, 'name']} rules={[{ required: true }]} noStyle>
                      <Input placeholder="工序名" style={{ width: 140 }} />
                    </Form.Item>
                    <Form.Item name={[f.name, 'workCenterId']} rules={[{ required: true }]} noStyle>
                      <Select placeholder="工作中心" style={{ width: 160 }} options={wcs.map((w) => ({ value: w.id, label: w.name }))} />
                    </Form.Item>
                    <Form.Item name={[f.name, 'prepTimeSec']} noStyle>
                      <InputNumber placeholder="准备(秒)" min={0} style={{ width: 100 }} />
                    </Form.Item>
                    <Form.Item name={[f.name, 'runTimePerUnitSec']} noStyle>
                      <InputNumber placeholder="单件加工(秒)" min={0} style={{ width: 120 }} />
                    </Form.Item>
                    <a onClick={() => remove(f.name)}>删除</a>
                  </Space>
                ))}
                <Button block onClick={() => add({})}>＋ 添加工序</Button>
              </>
            )}
          </Form.List>
        </Form>
      </Modal>
    </Card>
  )
}
