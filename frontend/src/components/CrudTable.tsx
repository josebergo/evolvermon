import { useCallback, useEffect, useRef, useState } from 'react'
import { Button, Card, Form, Input, Modal, Popconfirm, Select, Space, Table } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { PlusOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import { del, get, post, put } from '../api/client'

/**
 * 通用 CRUD 列表引擎（架构 §3：模块页开发成本压到小时级）。
 * 传入 columns + api 即获得：搜索/过滤/分页/新增/编辑/删除。
 */
export interface CrudTableProps<T = any> {
  title?: string
  api: string                       // 资源路径，如 /materials
  columns: ColumnsType<T>
  formItems?: (form: any, editing: any) => JSX.Element | JSX.Element[]  // 新增/编辑表单
  beforeSubmit?: (values: any, editing: any) => any                     // 提交前加工
  rowActions?: (row: any, reload: () => void) => JSX.Element | JSX.Element[] | null // 行扩展操作
  searchPlaceholder?: string
  statusOptions?: { value: string; label: string }[]
  filters?: Record<string, string>  // 额外固定过滤参数
  hideAdd?: boolean
  hideDelete?: boolean
  defaultPageSize?: number
}

export default function CrudTable<T extends { id?: number }>(props: CrudTableProps<T>) {
  const { title, api, columns, formItems, beforeSubmit, rowActions,
    searchPlaceholder, statusOptions, filters = {}, hideAdd, hideDelete, defaultPageSize = 20 } = props
  const [rows, setRows] = useState<T[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [page, setPage] = useState(1)
  const [size, setSize] = useState(defaultPageSize)
  const [q, setQ] = useState('')
  const [status, setStatus] = useState('')
  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<any>(null)
  const [form] = Form.useForm()
  const reqSeq = useRef(0)

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      const seq = ++reqSeq.current
      const data: any = await get(api, { page, page_size: size, q: q || undefined, status: status || undefined, ...filters })
      if (seq !== reqSeq.current) return
      setRows(data?.list || [])
      setTotal(data?.total || 0)
    } finally { setLoading(false) }
  }, [api, page, size, q, status, JSON.stringify(filters)])

  useEffect(() => { reload() }, [reload])

  const openCreate = () => { setEditing(null); form.resetFields(); setModalOpen(true) }
  const openEdit = (row: any) => { setEditing(row); form.setFieldsValue(row); setModalOpen(true) }

  const submit = async () => {
    const values = await form.validateFields()
    const payload = beforeSubmit ? beforeSubmit(values, editing) : values
    if (editing) {
      await put(`${api}/${editing.id}`, { ...editing, ...payload })
    } else {
      await post(api, payload)
    }
    setModalOpen(false)
    reload()
  }

  const actionCol: ColumnsType<T>[number] = {
    title: '操作', width: 180, fixed: 'right',
    render: (_: any, row: any) => (
      <Space size={4}>
        {formItems && <a onClick={() => openEdit(row)}>编辑</a>}
        {rowActions?.(row, reload)}
        {!hideDelete && (
          <Popconfirm title="确认删除？" onConfirm={async () => { await del(`${api}/${row.id}`); reload() }}>
            <a style={{ color: '#cf1322' }}>删除</a>
          </Popconfirm>
        )}
      </Space>
    ),
  }

  return (
    <Card title={title} styles={{ body: { padding: 12 } }}
      extra={
        <Space>
          {statusOptions && statusOptions.length > 0 && (
            <Select allowClear placeholder="状态" style={{ width: 120 }} value={status || undefined}
              options={statusOptions} onChange={(v) => { setPage(1); setStatus(v || '') }} />
          )}
          <Input allowClear prefix={<SearchOutlined />} placeholder={searchPlaceholder || '搜索'} style={{ width: 220 }}
            value={q} onChange={(e) => { setPage(1); setQ(e.target.value) }} />
          <Button icon={<ReloadOutlined />} onClick={reload}>刷新</Button>
          {formItems && !hideAdd && <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>新增</Button>}
        </Space>
      }>
      <Table<T> rowKey={(r: any) => r.id} loading={loading} columns={[...columns, actionCol]}
        dataSource={rows} size="middle" scroll={{ x: 'max-content' }}
        pagination={{ current: page, pageSize: size, total, showSizeChanger: true, showTotal: (t) => `共 ${t} 条`,
          onChange: (p, s) => { setPage(p); setSize(s) } }} />
      <Modal title={editing ? '编辑' : '新增'} open={modalOpen} onOk={submit} onCancel={() => setModalOpen(false)}
        destroyOnClose width={720}>
        <Form form={form} layout="vertical" style={{ maxHeight: '60vh', overflow: 'auto', paddingRight: 8 }}>
          {formItems?.(form, editing)}
        </Form>
      </Modal>
    </Card>
  )
}
