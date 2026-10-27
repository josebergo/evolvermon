import { useEffect, useState } from 'react'
import { Button, Card, DatePicker, Drawer, Form, Input, InputNumber, Modal, Progress, Select, Space, Table, Tag, message } from 'antd'
import { get, post } from '../../api/client'

const stMap: Record<string, { t: string; c: string }> = {
  draft: { t: '草稿', c: 'default' }, released: { t: '已下达', c: 'blue' }, running: { t: '执行中', c: 'processing' },
  paused: { t: '暂停', c: 'orange' }, finished: { t: '已完工', c: 'cyan' }, stored: { t: '已入库', c: 'green' },
  closed: { t: '已关闭', c: 'default' }, void: { t: '作废', c: 'red' },
}

export default function WorkOrders() {
  const [rows, setRows] = useState<any[]>([])
  const [mats, setMats] = useState<any[]>([])
  const [users, setUsers] = useState<any[]>([])
  const [creating, setCreating] = useState(false)
  const [detail, setDetail] = useState<any>(null)
  const [detailTab, setDetailTab] = useState<'ops' | 'reports'>('ops')
  const [reports, setReports] = useState<any[]>([])
  const [reporting, setReporting] = useState<any>(null)
  const [form] = Form.useForm()
  const [reportForm] = Form.useForm()

  const reload = async () => setRows((await get('/work-orders', { page_size: 50 }))?.list || [])
  useEffect(() => {
    reload()
    get('/materials', { page_size: 200 }).then((d: any) => setMats(d.list || []))
    get('/users', { page_size: 100 }).then((d: any) => setUsers(d.list || []))
  }, [])

  const matName = (id: number) => {
    const m = mats.find((x) => x.id === id)
    return m ? `${m.code} ${m.name}` : id
  }

  const openDetail = async (r: any) => {
    const full: any = await get(`/work-orders/${r.id}`)
    setDetail(full)
    setDetailTab('ops')
    get('/production-reports', { work_order_id: r.id }).then((d: any) => setReports(d.list || []))
  }

  const act = async (r: any, action: string, tip: string) => {
    await post(`/work-orders/${r.id}/${action}`)
    message.success(tip)
    reload()
    if (detail?.id === r.id) openDetail(r)
  }

  const save = async () => {
    const v = await form.validateFields()
    await post('/work-orders', {
      materialId: Number(v.materialId), qty: v.qty, priority: v.priority ?? 5,
      planStartAt: v.planStartAt?.$d, planEndAt: v.planEndAt?.$d, remark: v.remark,
    })
    setCreating(false); form.resetFields(); reload()
  }

  const submitReport = async () => {
    const v = await reportForm.validateFields()
    await post('/production-reports', {
      workOrderId: detail.id, operationId: reporting.id,
      qtyOk: v.qtyOk, qtyDefect: v.qtyDefect || 0, defectCodes: v.defectCodes || [],
      hours: v.hours || 0, clientToken: crypto.randomUUID(),
    })
    message.success('报工成功')
    setReporting(null); reportForm.resetFields()
    openDetail(detail); reload()
  }

  return (
    <Card title="工单管理（下达 → 领料 → 报工 → 完工 → 入库）" extra={<Space>
      <Button onClick={reload}>刷新</Button>
      <Button type="primary" onClick={() => setCreating(true)}>新建工单</Button>
    </Space>}>
      <Table rowKey="id" size="middle" dataSource={rows} pagination={{ pageSize: 15 }}
        columns={[
          { title: '工单号', dataIndex: 'docNo', width: 150 },
          { title: '物料', dataIndex: 'materialId', render: (v) => matName(Number(v)) },
          { title: '计划量', dataIndex: 'qty', width: 90 },
          { title: '合格量', dataIndex: 'goodQty', width: 90 },
          { title: '进度', width: 140, render: (_, r) => <Progress percent={Math.min(100, Math.round((r.goodQty / r.qty) * 100) || 0)} size="small" /> },
          { title: '状态', dataIndex: 'status', width: 90, render: (v) => <Tag color={stMap[v]?.c}>{stMap[v]?.t || v}</Tag> },
          { title: '计划完工', dataIndex: 'planEndAt', width: 105, render: (v) => v?.slice(0, 10) },
          {
            title: '操作', width: 330, render: (_, r) => (
              <Space size={4} wrap>
                <a onClick={() => openDetail(r)}>详情</a>
                <a onClick={() => window.open(`/api/v1/print/work-orders/${r.id}?access_token=${localStorage.getItem('em_at')}`, '_blank')}>路由卡</a>
                {r.status === 'draft' && <a onClick={() => act(r, 'release', '已下达并生成工序')}>下达</a>}
                {['released', 'running'].includes(r.status) && (
                  <>
                    <a onClick={async () => { const o = await post(`/work-orders/${r.id}/picking-request`); message.success(`已生成领料单 ${o.docNos?.join('、') || o.docNo}（原料/半成品分仓）`) }}>领料</a>
                    {r.status === 'running' ? <a onClick={() => act(r, 'pause', '已暂停')}>暂停</a> : <a onClick={() => act(r, 'resume', '已恢复')}>恢复</a>}
                  </>
                )}
                {['released', 'running'].includes(r.status) && <a onClick={() => act(r, 'force-finish', '已强制完工')}>强制完工</a>}
                {r.status === 'finished' && (
                  <a onClick={async () => { const o = await post(`/work-orders/${r.id}/finish-receipt`); message.success(`已生成报交入库单 ${o.docNo}，请到仓储管理过账`) }}>报交入库</a>
                )}
                {r.status === 'draft' && <a style={{ color: '#cf1322' }} onClick={() => act(r, 'void', '已作废')}>作废</a>}
                {['finished', 'stored'].includes(r.status) && <a onClick={() => act(r, 'close', '已关闭')}>关闭</a>}
              </Space>
            ),
          },
        ]} />

      <Modal title="新建工单" open={creating} onOk={save} onCancel={() => setCreating(false)} destroyOnClose>
        <Form form={form} layout="vertical">
          <Form.Item name="materialId" label="成品/半成品物料" rules={[{ required: true }]}>
            <Select showSearch optionFilterProp="label" style={{ width: '100%' }}
              options={mats.filter((m) => m.sourceType === 'make').map((m) => ({ value: m.id, label: `${m.code} ${m.name}` }))} />
          </Form.Item>
          <Form.Item name="qty" label="计划数量" rules={[{ required: true }]}><InputNumber min={1} style={{ width: '100%' }} /></Form.Item>
          <Space>
            <Form.Item name="planStartAt" label="计划开工"><DatePicker showTime /></Form.Item>
            <Form.Item name="planEndAt" label="计划完工"><DatePicker showTime /></Form.Item>
          </Space>
          <Form.Item name="remark" label="备注"><Input /></Form.Item>
        </Form>
      </Modal>

      <Drawer title={`工单 ${detail?.docNo || ''}`} open={!!detail} onClose={() => setDetail(null)} width={760}>
        {detail && (
          <>
            <Space wrap style={{ marginBottom: 12 }}>
              <Tag color={stMap[detail.status]?.c}>{stMap[detail.status]?.t}</Tag>
              <span>物料：{matName(detail.materialId)}</span>
              <span>计划 {detail.qty} · 合格 {detail.goodQty} · 不良 {detail.defectQty}</span>
            </Space>
            <Space style={{ marginBottom: 12 }}>
              <Button size="small" type={detailTab === 'ops' ? 'primary' : 'default'} onClick={() => setDetailTab('ops')}>工序列序</Button>
              <Button size="small" type={detailTab === 'reports' ? 'primary' : 'default'} onClick={() => setDetailTab('reports')}>报工记录</Button>
              <Button size="small" onClick={async () => {
                const trace: any = await get(`/trace/work-order/${detail.id}`)
                Modal.info({ title: '质量追溯链', width: 640, content: (
                  <div>
                    <div>报工 {trace.reports?.length || 0} 条；领料单 {trace.pickings?.length || 0} 张；批次流 {trace.batchFlow?.length || 0} 条</div>
                    {(trace.batchFlow || []).map((m: any, i: number) => (
                      <div key={i}>{m.direction === 'IN' ? '入库' : '出库'} {m.qty}（{m.bizType}）批次 {m.batchNo || '-'} @ {m.sourceDocNo}</div>
                    ))}
                  </div>
                ) })
              }}>追溯</Button>
            </Space>
            {detailTab === 'ops' ? (
              <Table rowKey="id" size="small" pagination={false} dataSource={detail.ops || []}
                columns={[
                  { title: '序', dataIndex: 'seq', width: 50 },
                  { title: '工序', dataIndex: 'name' },
                  { title: '状态', dataIndex: 'status', width: 90, render: (v) => <Tag>{({ waiting: '待派工', assigned: '已派工', running: '执行中', done: '已完成', skipped: '跳过' } as any)[v] || v}</Tag> },
                  { title: '合格/不良', width: 120, render: (_: any, o: any) => `${o.qtyOk}/${o.qtyDefect}` },
                  {
                    title: '操作', width: 200, render: (_: any, o: any) => (
                      <Space size={4}>
                        <Select size="small" placeholder="派人" style={{ width: 110 }} showSearch optionFilterProp="label"
                          value={o.assigneeUserId || undefined}
                          options={users.map((u) => ({ value: u.id, label: u.realName || u.username }))}
                          onChange={async (v) => { await post(`/wo-operations/${o.id}/assign`, { userId: v }); openDetail(detail) }} />
                        {['waiting', 'assigned'].includes(o.status) && <a onClick={async () => { await post(`/wo-operations/${o.id}/start`); openDetail(detail) }}>开工</a>}
                        {['waiting', 'assigned', 'running'].includes(o.status) && <a onClick={() => { setReporting(o); reportForm.resetFields() }}>报工</a>}
                      </Space>
                    ),
                  },
                ]} />
            ) : (
              <Table rowKey="id" size="small" pagination={false} dataSource={reports}
                columns={[
                  { title: '时间', dataIndex: 'reportedAt', width: 150, render: (v) => v?.slice(0, 19).replace('T', ' ') },
                  { title: '操作工', dataIndex: 'username', width: 90 },
                  { title: '合格', dataIndex: 'qtyOk', width: 70 },
                  { title: '不良', dataIndex: 'qtyDefect', width: 70 },
                  { title: '不良代码', dataIndex: 'defectCodes', width: 120 },
                  { title: '工时', dataIndex: 'hours', width: 70 },
                  { title: '备注', dataIndex: 'remark' },
                ]} />
            )}
          </>
        )}
      </Drawer>

      <Modal title={`报工：${reporting?.name || ''}（工单 ${detail?.docNo}）`} open={!!reporting} onOk={submitReport}
        onCancel={() => setReporting(null)} destroyOnClose>
        <Form form={reportForm} layout="vertical">
          <Form.Item name="qtyOk" label="合格数" rules={[{ required: true }]}><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="qtyDefect" label="不良数" initialValue={0}><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="defectCodes" label="不良代码">
            <Select mode="multiple" options={[{ value: 'D001', label: 'D001 外观划伤' }, { value: 'D002', label: 'D002 尺寸超差' }, { value: 'D003', label: 'D003 焊点虚焊' }, { value: 'D004', label: 'D004 功能不良' }, { value: 'D005', label: 'D005 来料不良' }]} />
          </Form.Item>
          <Form.Item name="hours" label="工时(小时)" initialValue={0}><InputNumber min={0} step={0.5} style={{ width: '100%' }} /></Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
