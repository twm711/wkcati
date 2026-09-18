import { useCallback, useEffect, useState } from 'react'
import { Card, Table, Tag, Button, Space, Select, Drawer, Descriptions, App, Modal, Input, Typography } from 'antd'
import { api, hasRole } from '../api'

interface Wo {
  id: number; caller_no: string; subject: string; detail: string; status: string; priority: string
  remark: string; revisit_sample_id: unknown; created_at: string; agent_name: unknown
}
interface Revisit { sampleId: number; custName: string; status: string; sheet: { id: number; status: string; qnr_version: string } | null }
interface WoDetail extends Wo { revisit?: Revisit | null }

const STATUS_COLOR: Record<string, string> = { PENDING: 'orange', ACCEPTED: 'processing', RESOLVED: 'cyan', CLOSED: 'green' }
const FLOW: Record<string, string> = { PENDING: '待受理', ACCEPTED: '处理中', RESOLVED: '已办结', CLOSED: '已归档' }

export default function Workorders() {
  const { message } = App.useApp()
  const [rows, setRows] = useState<Wo[]>([])
  const [status, setStatus] = useState<string>('')
  const [detail, setDetail] = useState<WoDetail | null>(null)
  const [action, setAction] = useState<'resolve' | null>(null)
  const [remark, setRemark] = useState('')
  const canClose = hasRole('groupAdmin')

  const load = useCallback(async () => {
    const r = await api.get<{ total: number; rows: Wo[] }>(`/api/workorder${status ? `?status=${status}` : ''}`)
    if (r.success) setRows(r.data.rows)
  }, [status])

  useEffect(() => {
    load()
  }, [load])

  const openDetail = async (tid: number) => {
    const r = await api.get<WoDetail>(`/api/workorder/${tid}`)
    if (r.success) setDetail(r.data)
  }

  const doAction = async (tid: number, act: 'accept' | 'resolve' | 'close') => {
    try {
      const r = await api.post<Record<string, unknown>>(`/api/workorder/${tid}/${act}`, act === 'resolve' ? { remark: remark || '已处理' } : {})
      if (!r.success) throw new Error(r.message)
      if (act === 'close') {
        message.success(`工单已归档${r.data.revisitSampleId ? `，自动生成回访样本 #${r.data.revisitSampleId}（项目 1 样本池）` : ''}`)
      } else {
        message.success(r.message)
      }
      setAction(null)
      setRemark('')
      setDetail(null)
      load()
    } catch (e) {
      message.error(e instanceof Error ? e.message : '操作失败')
    }
  }

  return (
    <Card
      title="工单中心（呼入转人工 → 落单 → 受理/办结/归档 → 自动回访样本）"
      extra={
        <Space>
          <Select
            style={{ width: 140 }}
            allowClear
            placeholder="按状态筛选"
            value={status || undefined}
            onChange={(v) => setStatus(v ?? '')}
            options={Object.keys(FLOW).map((k) => ({ value: k, label: FLOW[k] }))}
          />
          <Button onClick={load}>刷新</Button>
        </Space>
      }
    >
      <Table<Wo>
        rowKey="id"
        size="small"
        pagination={{ pageSize: 8 }}
        dataSource={rows}
        columns={[
          { title: '工单', dataIndex: 'id', width: 70 },
          { title: '来电号码', dataIndex: 'caller_no' },
          { title: '主题', dataIndex: 'subject' },
          { title: '状态', dataIndex: 'status', render: (v) => <Tag color={STATUS_COLOR[v] ?? 'default'}>{FLOW[v] ?? v}</Tag> },
          { title: '优先级', dataIndex: 'priority', width: 90 },
          { title: '受理坐席', dataIndex: 'agent_name', render: (v) => String(v ?? '-') },
          { title: '回访样本', dataIndex: 'revisit_sample_id', render: (v) => (v ? <Tag color="purple">#{String(v)}</Tag> : '-') },
          { title: '创建', dataIndex: 'created_at', render: (v) => String(v).slice(0, 19).replace('T', ' ') },
          {
            title: '操作', width: 160,
            render: (_, row) => (
              <Space>
                <Button size="small" onClick={() => openDetail(row.id)}>详情</Button>
                {row.status === 'PENDING' && <Button size="small" type="primary" ghost onClick={() => doAction(row.id, 'accept')}>受理</Button>}
              </Space>
            ),
          },
        ]}
      />
      <Drawer
        open={!!detail}
        width={480}
        onClose={() => setDetail(null)}
        title={detail ? `工单 #${detail.id}` : ''}
      >
        {detail && (
          <Space direction="vertical" size={12} style={{ width: '100%' }}>
            <Descriptions bordered size="small" column={1}
              items={[
                { key: 'c', label: '来电号码', children: detail.caller_no },
                { key: 's', label: '主题', children: detail.subject },
                { key: 'd', label: '详情', children: detail.detail },
                { key: 'st', label: '状态', children: <Tag color={STATUS_COLOR[detail.status]}>{FLOW[detail.status] ?? detail.status}</Tag> },
                { key: 'p', label: '优先级', children: detail.priority },
                { key: 'r', label: '备注', children: detail.remark || '-' },
                { key: 'a', label: '受理坐席', children: String(detail.agent_name ?? '-') },
                { key: 't', label: '创建时间', children: detail.created_at },
              ]}
            />
            {detail.revisit && (
              <Card size="small" title="回访链路（归档自动生成）">
                <Typography.Paragraph style={{ marginBottom: 4 }}>
                  样本 #{detail.revisit.sampleId} · {detail.revisit.custName} · <Tag>{detail.revisit.status}</Tag>
                </Typography.Paragraph>
                {detail.revisit.sheet ? (
                  <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 0 }}>
                    回访答卷 #{detail.revisit.sheet.id} · {detail.revisit.sheet.status} · {detail.revisit.sheet.qnr_version}
                  </Typography.Paragraph>
                ) : (
                  <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 0 }}>尚未外呼回访</Typography.Paragraph>
                )}
              </Card>
            )}
            <Space wrap>
              {detail.status === 'PENDING' && <Button type="primary" onClick={() => doAction(detail.id, 'accept')}>受理</Button>}
              {detail.status === 'ACCEPTED' && <Button type="primary" onClick={() => setAction('resolve')}>办结</Button>}
              {detail.status === 'RESOLVED' && canClose && (
                <Button type="primary" danger onClick={() => doAction(detail.id, 'close')}>归档（督导 · 生成回访样本）</Button>
              )}
              {detail.status === 'RESOLVED' && !canClose && (
                <Typography.Text type="secondary">归档需督导（groupAdmin）权限</Typography.Text>
              )}
            </Space>
          </Space>
        )}
      </Drawer>
      <Modal
        open={!!action}
        title="办结工单"
        onCancel={() => setAction(null)}
        onOk={() => detail && doAction(detail.id, 'resolve')}
        okText="办结"
      >
        <Input.TextArea rows={3} value={remark} onChange={(e) => setRemark(e.target.value)} placeholder="处理结果备注" />
      </Modal>
    </Card>
  )
}
