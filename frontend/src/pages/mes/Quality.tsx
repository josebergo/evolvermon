import { useEffect, useState } from 'react'
import { Button, Card, Form, Input, InputNumber, Modal, Select, Space, Table, Tag, message } from 'antd'
import { get, post } from '../../api/client'

// 质检管理：质检单创建 + 判定（FR-MES-040~044）
export default function Quality() {
  const [rows, setRows] = useState<any[]>([])
  const [mats, setMats] = useState<any[]>([])
  const [wos, setWos] = useState<any[]>([])
  const [creating, setCreating] = useState(false)
  const [judging, setJudging] = useState<any>(null)
  const [form] = Form.useForm()
  const [judgeForm] = Form.useForm()

  const reload = async () => setRows((await get('/qc-records', { page_size: 50 }))?.list || [])
  useEffect(() => {
    reload()
    get('/materials', { page_size: 200 }).then((d: any) => setMats(d.list || []))
    get('/work-orders', { page_size: 50 }).then((d: any) => setWos(d.list || []))
  }, [])

  const matName = (id: number) => {
    const m = mats.find((x) => x.id === id)
    return m ? `${m.code} ${m.name}` : id
  }

  const save = async () => {
    const v = await form.validateFields()
    await post('/qc-records', { ...v, sourceType: v.workOrderId ? 'finish' : 'manual', qty: v.qty })
    setCreating(false); form.resetFields(); reload()
  }

  const judge = async () => {
    const v = await judgeForm.validateFields()
    await post(`/qc-records/${judging.id}/audit`, { result: v.result, disposal: v.disposal || '', itemsResult: v.itemsResult || '{}' })
    message.success('已判定')
    setJudging(null); reload()
  }

  return (
    <Card title="质检单（IQC 来料 / FQC 完工检验，不合格需处置）" extra={<Space>
      <Button onClick={reload}>刷新</Button>
      <Button type="primary" onClick={() => setCreating(true)}>新建质检单</Button>
    </Space>}>
      <Table rowKey="id" size="middle" dataSource={rows} pagination={{ pageSize: 15 }}
        columns={[
          { title: '质检单号', dataIndex: 'docNo', width: 150 },
          { title: '来源', dataIndex: 'sourceType', width: 80, render: (v) => ({ arrive: '来料', operation: '工序', finish: '完工', manual: '手工' } as any)[v] || v },
          { title: '来源单号', dataIndex: 'sourceDocNo', width: 140 },
          { title: '物料', dataIndex: 'materialId', render: (v) => matName(Number(v)) },
          { title: '批次', dataIndex: 'batchNo', width: 120 },
          { title: '数量', dataIndex: 'qty', width: 80 },
          { title: '结果', dataIndex: 'result', width: 90, render: (v: string) => <Tag color={({ pending: 'default', pass: 'green', fail: 'red', concession: 'orange' } as Record<string, string>)[v]}>{({ pending: '待检', pass: '合格', fail: '不合格', concession: '让步' } as Record<string, string>)[v] || v}</Tag> },
          { title: '处置', dataIndex: 'disposal', width: 80, render: (v) => ({ rework: '返工', scrap: '报废', concession: '让步' } as any)[v] || '' },
          {
            title: '操作', width: 130, render: (_, r) => (
              <Space size={4}>
                {r.result === 'pending' && <a onClick={() => { setJudging(r); judgeForm.resetFields() }}>判定</a>}
                {r.result === 'fail' && r.disposal && r.disposal !== 'concession' && (
                  <a style={{ color: '#cf1322' }} onClick={async () => {
                    const res: any = await post(`/qc-records/${r.id}/dispose`)
                    message.success(r.disposal === 'rework' ? `已生成返工工单 ${res.reworkWorkOrder}` : `已生成报废移库单 ${res.scrapMoveOrder}，请到仓储管理过账`)
                    reload()
                  }}>执行处置</a>
                )}
              </Space>
            ),
          },
        ]} />

      <Modal title="新建质检单" open={creating} onOk={save} onCancel={() => setCreating(false)} destroyOnClose>
        <Form form={form} layout="vertical">
          <Form.Item name="materialId" label="物料" rules={[{ required: true }]}>
            <Select showSearch optionFilterProp="label" style={{ width: '100%' }}
              options={mats.map((m) => ({ value: m.id, label: `${m.code} ${m.name}` }))} />
          </Form.Item>
          <Form.Item name="workOrderId" label="关联工单（完工检）">
            <Select allowClear showSearch optionFilterProp="label" style={{ width: '100%' }}
              options={wos.map((w) => ({ value: w.id, label: w.docNo }))} />
          </Form.Item>
          <Form.Item name="batchNo" label="批次号"><Input /></Form.Item>
          <Form.Item name="qty" label="抽检数量" rules={[{ required: true }]}><InputNumber min={1} style={{ width: '100%' }} /></Form.Item>
        </Form>
      </Modal>

      <Modal title={`判定：${judging?.docNo || ''}`} open={!!judging} onOk={judge} onCancel={() => setJudging(null)} destroyOnClose>
        <Form form={judgeForm} layout="vertical">
          <Form.Item name="result" label="检验结果" rules={[{ required: true }]}>
            <Select options={[{ value: 'pass', label: '合格' }, { value: 'fail', label: '不合格' }, { value: 'concession', label: '让步放行' }]} />
          </Form.Item>
          <Form.Item name="disposal" label="不合格处置">
            <Select allowClear options={[{ value: 'rework', label: '返工' }, { value: 'scrap', label: '报废' }, { value: 'concession', label: '让步' }]} />
          </Form.Item>
          <Form.Item name="itemsResult" label="检验明细(JSON)"><Input.TextArea rows={3} placeholder='{"外观":"OK","尺寸":"OK"}' /></Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
