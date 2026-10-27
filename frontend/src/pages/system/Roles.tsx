import { useEffect, useState } from 'react'
import { Button, Card, Form, Input, Modal, Select, Space, Table, Tree, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { get, post, put } from '../../api/client'

// 角色权限：角色 CRUD + 权限点勾选（FR-PLT-021）
export default function Roles() {
  const [rows, setRows] = useState<any[]>([])
  const [perms, setPerms] = useState<any[]>([])
  const [editing, setEditing] = useState<any>(null)
  const [formOpen, setFormOpen] = useState(false)
  const [permOpen, setPermOpen] = useState(false)
  const [checked, setChecked] = useState<string[]>([])
  const [form] = Form.useForm()

  const reload = async () => setRows((await get('/roles', { page_size: 100 }))?.list || [])
  useEffect(() => { reload(); get('/perms').then(setPerms) }, [])

  const treeData = buildPermTree(perms)

  const save = async () => {
    const v = await form.validateFields()
    if (editing) await put(`/roles/${editing.id}`, { ...editing, ...v })
    else await post('/roles', v)
    setEditing(null); setFormOpen(false); form.resetFields(); reload()
  }

  const savePerms = async () => {
    await put(`/roles/${editing.id}/perms`, { permCodes: checked, dataScope: editing.dataScope })
    message.success('已保存')
    setPermOpen(false); reload()
  }

  const columns: ColumnsType<any> = [
    { title: '编码', dataIndex: 'code', width: 160 },
    { title: '名称', dataIndex: 'name', width: 140 },
    { title: '数据范围', dataIndex: 'dataScope', width: 100, render: (v) => ({ tenant: '本租户', factory: '本工厂', self: '本人' } as any)[v] || v },
    { title: '内置', dataIndex: 'builtIn', width: 70, render: (v) => (v ? '是' : '') },
    { title: '状态', dataIndex: 'status', width: 80, render: (v) => (v === 1 ? '启用' : '停用') },
    {
      title: '操作', width: 160, render: (_, r) => (
        <Space>
          <a onClick={() => { form.setFieldsValue(r); setEditing(r); setFormOpen(true) }}>编辑</a>
          <a onClick={() => { setEditing(r); setPermOpen(true) }}>权限</a>
        </Space>
      ),
    },
  ]

  return (
    <Card title="角色权限管理" extra={<Button type="primary" onClick={() => { setEditing(null); form.resetFields(); setFormOpen(true) }}>新增角色</Button>}>
      <Table rowKey="id" size="middle" columns={columns} dataSource={rows} pagination={{ pageSize: 20 }} />
      <Modal title={editing ? '编辑角色' : '新增角色'} open={formOpen}
        onCancel={() => { setEditing(null); setFormOpen(false); form.resetFields() }} onOk={save} destroyOnClose>
        <Form form={form} layout="vertical">
          <Form.Item name="code" label="角色编码" rules={[{ required: true }]}><Input disabled={!!editing?.builtIn} /></Form.Item>
          <Form.Item name="name" label="角色名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="dataScope" label="数据范围" initialValue="factory">
            <Select options={[{ value: 'tenant', label: '本租户' }, { value: 'factory', label: '本工厂' }, { value: 'self', label: '本人' }]} />
          </Form.Item>
          <Form.Item name="status" label="状态" initialValue={1}><Select options={[{ value: 1, label: '启用' }, { value: 0, label: '停用' }]} /></Form.Item>
        </Form>
      </Modal>
      <Modal title={`权限配置：${editing?.name || ''}`} open={permOpen} onOk={savePerms} onCancel={() => setPermOpen(false)} width={560}>
        <Tree checkable defaultExpandAll treeData={treeData}
          onCheck={(keys: any) => setChecked((keys.checked || keys) as string[])} />
        <div style={{ marginTop: 12 }}>
          数据范围：
          <Select size="small" style={{ width: 120, marginLeft: 8 }} value={editing?.dataScope || 'factory'}
            onChange={(v) => setEditing({ ...editing, dataScope: v })}
            options={[{ value: 'tenant', label: '本租户' }, { value: 'factory', label: '本工厂' }, { value: 'self', label: '本人' }]} />
        </div>
      </Modal>
    </Card>
  )
}

function buildPermTree(perms: any[]) {
  const groups: Record<string, any[]> = {}
  for (const p of perms) {
    const mod = p.code.split(':')[0]
    ;(groups[mod] = groups[mod] || []).push({ title: p.name, key: p.code })
  }
  const modNames: Record<string, string> = {
    plt: '平台', erp: 'ERP', mes: 'MES', wms: 'WMS',
  }
  return Object.entries(groups).map(([mod, children]) => ({
    title: `${modNames[mod] || mod}（${mod}:* 全部）`, key: `${mod}:*`, children,
  }))
}
