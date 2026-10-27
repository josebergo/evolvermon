import { useEffect, useState } from 'react'
import { Button, Card, Descriptions, InputNumber, Select, Space, Tag, Typography, message } from 'antd'
import { get } from '../../api/client'

// ATP 可承诺量（M3）：销售接单前回答“能不能按期交”
export default function Atp() {
  const [mats, setMats] = useState<any[]>([])
  const [matId, setMatId] = useState<number>()
  const [qty, setQty] = useState<number>(100)
  const [result, setResult] = useState<any>(null)

  useEffect(() => {
    get('/materials?category=finished&page_size=100').then((d: any) => {
      setMats(d.list || [])
      if (d.list?.[0]) setMatId(d.list[0].id)
    })
  }, [])

  const run = async () => {
    if (!matId) return
    const data: any = await get('/atp', { material_id: matId, qty })
    setResult(data)
  }

  return (
    <Card title="ATP 可承诺量（现有 −预占 +在途 +在制 −在手需求）">
      <Space size={12}>
        <Select style={{ width: 260 }} showSearch optionFilterProp="label" value={matId} onChange={setMatId}
          options={mats.map((m) => ({ value: m.id, label: `${m.code} ${m.name}` }))} />
        <InputNumber min={1} value={qty} onChange={(v) => setQty(v || 1)} />
        <Button type="primary" onClick={run}>查询可承诺</Button>
      </Space>
      {result && (
        <Descriptions bordered size="small" column={4} style={{ marginTop: 16 }}>
          <Descriptions.Item label="现存量">{result.onHand}</Descriptions.Item>
          <Descriptions.Item label="预占">{result.reserved}</Descriptions.Item>
          <Descriptions.Item label="在途采购">{result.inTransit}</Descriptions.Item>
          <Descriptions.Item label="在制">{result.wip}</Descriptions.Item>
          <Descriptions.Item label="其他在手需求">{result.otherDemand}</Descriptions.Item>
          <Descriptions.Item label="可承诺量"><b style={{ fontSize: 16 }}>{result.available}</b></Descriptions.Item>
          <Descriptions.Item label="能否满足">
            {result.feasible ? <Tag color="green">可满足</Tag> : <Tag color="red">缺口 {Number(result.qty) - Number(result.available)}</Tag>}
          </Descriptions.Item>
          <Descriptions.Item label="建议承诺日期"><b>{result.earliestDate}</b></Descriptions.Item>
        </Descriptions>
      )}
      <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>
        计算口径：可承诺 = 现有库存 − 已预占 + 在途采购 + 在制产出 − 其他已审核订单未发量；无法满足时按物料采购/生产提前期给出建议交期。
      </Typography.Paragraph>
    </Card>
  )
}
