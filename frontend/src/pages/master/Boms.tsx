import { useEffect, useState } from 'react'
import { Button, Card, Form, Input, InputNumber, Modal, Select, Space, Table, message } from 'antd'
import { get, post, put } from '../../api/client'

// BOM 管理：列表 + 行编辑 + 启用切换 + 树形展开（FR-ERP-020~027）
export default function Boms() {
  const [rows, setRows] = useState<any[]>([])
  const [mats, setMats] = useState<any[]>([])
  const [editing, setEditing] = useState<any>(null) // { id?, materialId, versionNo, lines: [] }
  const [form] = Form.useForm()

  const reload = async () => setRows((await get('/boms', { page_size: 100 }))?.list || [])
  useEffect(() => {
    reload()
    get('/materials', { page_size: 200 }).then((d: any) => setMats(d.list || []))
  }, [])

  const matName = (id: number) => {
    const m = mats.find((x) => x.id === id)
    return m ? `${m.code} ${m.name}` : id
  }

  const openEdit = (r?: any) => {
    const data = r ? {
      id: r.id, materialId: r.materialId, versionNo: r.versionNo,
      lines: (r.lines || []).map((l: any) => ({ ...l })),
    } : { materialId: undefined, versionNo: 'A', lines: [{}] }
    setEditing(data)
    form.setFieldsValue(data)
  }

  const save = async () => {
    const v = await form.validateFields()
    const payload = { materialId: Number(v.materialId), versionNo: v.versionNo, lines: v.lines }
    if (editing.id) {
      await put(`/boms/${editing.id}`, { ...editing, ...payload, lines: payload.lines })
    } else {
      await post('/boms', payload)
    }
    setEditing(null); reload()
  }

  return (
    <Card title="BOM 物料清单（同物料仅一个启用版本）" extra={
      <Space>
        <Button onClick={reload}>刷新</Button>
        <Button type="primary" onClick={() => openEdit()}>新增 BOM</Button>
      </Space>
    }>
      <Table rowKey="id" size="middle" dataSource={rows} pagination={{ pageSize: 20 }}
        columns={[
          { title: 'ID', dataIndex: 'id', width: 60 },
          { title: '父项物料', dataIndex: 'materialId', render: (v) => matName(Number(v)) },
          { title: '版本', dataIndex: 'versionNo', width: 80 },
          { title: '状态', dataIndex: 'status', width: 90, render: (v) => ({ draft: '草稿', active: '启用', off: '停用' } as any)[v] || v },
          { title: '子项数', dataIndex: 'lines', width: 80, render: (v: any[]) => v?.length || 0 },
          {
            title: '操作', width: 220, render: (_, r) => (
              <Space>
                <a onClick={() => openEdit(r)}>编辑</a>
                {r.status !== 'active' && <a onClick={async () => { await post(`/boms/${r.id}/enable`); message.success('已启用'); reload() }}>启用</a>}
                <a onClick={async () => {
                  const tree: any = await get(`/boms/${r.id}/tree`)
                  Modal.info({
                    title: `BOM 树：${tree.code} ${tree.name}`,
                    width: 560,
                    content: renderTree(tree.children || []),
                  })
                }}>树形展开</a>
              </Space>
            ),
          },
        ]} />

      <Modal title={editing?.id ? '编辑 BOM' : '新增 BOM'} open={!!editing} onOk={save}
        onCancel={() => setEditing(null)} width={760} destroyOnClose>
        <Form form={form} layout="vertical">
          <Space size={16}>
            <Form.Item name="materialId" label="父项物料" rules={[{ required: true }]} style={{ width: 320 }}>
              <Select showSearch optionFilterProp="label"
                options={mats.map((m) => ({ value: m.id, label: `${m.code} ${m.name}` }))} />
            </Form.Item>
            <Form.Item name="versionNo" label="版本号" rules={[{ required: true }]} style={{ width: 120 }}>
              <Input placeholder="A" />
            </Form.Item>
          </Space>
          <Form.List name="lines">
            {(fields, { add, remove }) => (
              <>
                {fields.map((f) => (
                  <Space key={f.key} align="baseline" style={{ display: 'flex', marginBottom: 4 }}>
                    <Form.Item name={[f.name, 'materialId']} rules={[{ required: true, message: '选子项' }]} noStyle>
                      <Select showSearch placeholder="子项物料" style={{ width: 280 }} optionFilterProp="label"
                        options={mats.map((m) => ({ value: m.id, label: `${m.code} ${m.name}` }))} />
                    </Form.Item>
                    <Form.Item name={[f.name, 'qty']} rules={[{ required: true, message: '用量' }]} noStyle>
                      <InputNumber placeholder="用量" min={0.0001} style={{ width: 110 }} />
                    </Form.Item>
                    <Form.Item name={[f.name, 'scrapRate']} noStyle>
                      <InputNumber placeholder="损耗%" min={0} max={100} style={{ width: 90 }} />
                    </Form.Item>
                    <a onClick={() => remove(f.name)}>删除</a>
                  </Space>
                ))}
                <Button onClick={() => add({})} block>＋ 添加子项</Button>
              </>
            )}
          </Form.List>
        </Form>
      </Modal>
    </Card>
  )
}

function renderTree(nodes: any[]): JSX.Element {
  return (
    <ul style={{ paddingLeft: 16 }}>
      {nodes.map((n, i) => (
        <li key={i}>
          <b>{n.code}</b> {n.name} × {n.qty} {n.unit}
          {n.children?.length > 0 && renderTree(n.children)}
        </li>
      ))}
    </ul>
  )
}
