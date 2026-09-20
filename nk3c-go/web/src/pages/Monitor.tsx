import { useCallback, useEffect, useRef, useState } from 'react'
import { Card, Statistic, Row, Col, Tag, Table, Button, App, Input, Space, Typography, Modal, Popconfirm } from 'antd'
import { ReloadOutlined, WifiOutlined, LinkOutlined } from '@ant-design/icons'
import { api, hasRole, rawSession } from '../api'

interface WallAgent { agentNo: string; userName: string; state: string; sampleId: unknown; callId: unknown; userId?: number }
interface Wall { agents: WallAgent[]; summary: { dialCount: number; connectCount: number; successCount: number; abandonCount: number } }
interface WallFrame { type: string; data: Wall; ts: number }
interface CallRow { id: number; sample_id: unknown; cust_name: unknown; agent_no: string; status: string; result_code: unknown; begin_time: string; connect_time: unknown }
interface SheetRow { id: number; call_id: number; project_id: number; sample_id: number; agent_id: number; qnr_id: number; qnr_version: string; status: string; audit_remark: string }
interface QCEvent { type: 'qc'; event: string; data: Record<string, unknown>; ts: number }

const STATE_COLOR: Record<string, string> = { READY: 'default', DIALING: 'processing', TALKING: 'warning' }

export default function Monitor() {
  const { message } = App.useApp()
  const [wall, setWall] = useState<Wall | null>(null)
  const [calls, setCalls] = useState<CallRow[]>([])
  const [sheets, setSheets] = useState<SheetRow[]>([])
  const [auditTarget, setAuditTarget] = useState<{ row: SheetRow; action: 'PASS' | 'REJECT' } | null>(null)
  const [wsLive, setWsLive] = useState(false)
  const [qcWsLive, setQcWsLive] = useState(false)
  const [qcEvents, setQcEvents] = useState<QCEvent[]>([])
  const timer = useRef<ReturnType<typeof setInterval> | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const canAudit = hasRole('groupAdmin')

  // 轮询兜底：话务流水/审核队列无 WS 推送，墙面则优先走 WS
  const load = useCallback(async () => {
    if (!wsLive) {
      const w = await api.get<Wall>('/api/monitor/wall')
      if (w.success) setWall(w.data)
    }
    const c = await api.get<CallRow[]>('/api/monitor/calls?limit=12')
    if (c.success) setCalls(c.data as unknown as CallRow[])
    if (canAudit) {
      const s = await api.get<{ total: number; rows: SheetRow[] }>('/api/sheet?status=SUBMITTED')
      if (s.success) setSheets(s.data.rows)
    }
  }, [canAudit, wsLive])

  // 监控墙 WebSocket（?token= 鉴权，失败自动降级 5s 轮询）
  useEffect(() => {
    const s = rawSession()
    if (!s) return
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    let closed = false
    const ws = new WebSocket(`${proto}://${location.host}/api/ws/monitor?token=${encodeURIComponent(s.sessionId)}`)
    wsRef.current = ws
    ws.onopen = () => { if (!closed) setWsLive(true) }
    ws.onmessage = (ev) => {
      try {
        const frame = JSON.parse(ev.data as string) as WallFrame
        if (frame.type === 'wall') setWall(frame.data)
      } catch { /* 忽略坏帧 */ }
    }
    ws.onclose = () => { if (!closed) setWsLive(false) }
    ws.onerror = () => { if (!closed) setWsLive(false) }
    return () => {
      closed = true
      ws.close()
      wsRef.current = null
    }
  }, [])

  useEffect(() => {
    load()
    timer.current = setInterval(load, 5000)
    return () => {
      if (timer.current) clearInterval(timer.current)
    }
  }, [load])

  // 质检事件流：仅督导订阅，断线自动重连；事件按最新在前保留 50 条
  useEffect(() => {
    if (!canAudit) return
    const s = rawSession()
    if (!s) return
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    let closed = false
    let retry: ReturnType<typeof setTimeout> | null = null
    let ws: WebSocket | null = null
    const connect = () => {
      if (closed) return
      ws = new WebSocket(`${proto}://${location.host}/api/qc/ws?token=${encodeURIComponent(s.sessionId)}`)
      ws.onopen = () => setQcWsLive(true)
      ws.onmessage = (ev) => {
        try {
          const frame = JSON.parse(ev.data as string) as QCEvent
          if (frame.type === 'qc' && frame.event) {
            setQcEvents((old) => [frame, ...old].slice(0, 50))
          }
        } catch { /* 忽略坏帧 */ }
      }
      ws.onerror = () => setQcWsLive(false)
      ws.onclose = () => {
        setQcWsLive(false)
        if (!closed) retry = setTimeout(connect, 5000)
      }
    }
    connect()
    return () => {
      closed = true
      if (retry) clearTimeout(retry)
      ws?.close()
      setQcWsLive(false)
    }
  }, [canAudit])

  const forceCheckout = async (userId: number, agentNo: string) => {
    try {
      const r = await api.post<{ sessions: number; releasedSamples: number }>('/api/qc/force-checkout', { userId })
      if (!r.success) throw new Error(r.message)
      message.success(`已强签 ${agentNo}：吊销会话 ${r.data.sessions} 个，样本回池 ${r.data.releasedSamples} 个`)
      load()
    } catch (e) {
      message.error(e instanceof Error ? e.message : '强签失败')
    }
  }

  const doAudit = async (remark: string) => {
    if (!auditTarget) return
    try {
      const r = await api.post<{ sheetId: number; status: string; sampleDest?: string }>(
        `/api/sheet/${auditTarget.row.id}/audit`,
        { action: auditTarget.action, remark },
      )
      if (!r.success) throw new Error(r.message)
      message.success(`答卷 #${r.data.sheetId} → ${r.data.status}${r.data.sampleDest ? '，样本 → ' + r.data.sampleDest : ''}`)
      setAuditTarget(null)
      load()
    } catch (e) {
      message.error(e instanceof Error ? e.message : '审核失败')
    }
  }

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card
        title={
          <Space>
            监控墙
            <Tag color={wsLive ? 'green' : 'orange'} icon={wsLive ? <WifiOutlined /> : <LinkOutlined />}>
              {wsLive ? 'WebSocket 实时推送' : '5s 轮询'}
            </Tag>
          </Space>
        }
        extra={<Button icon={<ReloadOutlined />} onClick={load}>刷新</Button>}
      >
        <Row gutter={16} style={{ marginBottom: 16 }}>
          <Col span={6}><Statistic title="今日外呼" value={wall?.summary.dialCount ?? '-'} /></Col>
          <Col span={6}><Statistic title="累计接通" value={wall?.summary.connectCount ?? '-'} /></Col>
          <Col span={6}><Statistic title="完成样本" value={wall?.summary.successCount ?? '-'} /></Col>
          <Col span={6}><Statistic title="放弃呼叫" value={wall?.summary.abandonCount ?? '-'} /></Col>
        </Row>
        <Row gutter={16}>
          {(wall?.agents ?? []).map((a) => (
            <Col key={a.agentNo} span={6} style={{ marginBottom: 12 }}>
              <Card size="small">
                <Space direction="vertical" size={0}>
                  <Space>
                    <Typography.Text strong>{a.agentNo}</Typography.Text>
                    <Tag color={STATE_COLOR[a.state] ?? 'default'}>{a.state}</Tag>
                  </Space>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>{a.userName}</Typography.Text>
                  {a.sampleId != null && <Typography.Text style={{ fontSize: 12 }}>样本 #{String(a.sampleId)}</Typography.Text>}
                  {canAudit && a.userId != null && (
                    <Popconfirm
                      title={`强签 ${a.agentNo}？`}
                      description="注销其全部会话，占用样本回池（INCALL/ASSIGNED → IDLE）"
                      onConfirm={() => forceCheckout(a.userId as number, a.agentNo)}
                    >
                      <Button size="small" danger style={{ marginTop: 4 }}>强签</Button>
                    </Popconfirm>
                  )}
                </Space>
              </Card>
            </Col>
          ))}
        </Row>
      </Card>

      {canAudit && (
        <Card
          title={
            <Space>
              质检事件流
              <Tag color={qcWsLive ? 'green' : 'orange'} icon={qcWsLive ? <WifiOutlined /> : <LinkOutlined />}>
                {qcWsLive ? '实时' : '连接中/重连中'}
              </Tag>
            </Space>
          }
          extra={<Button size="small" onClick={() => setQcEvents([])}>清空</Button>}
        >
          <Table<QCEvent>
            rowKey={(row) => `${row.ts}-${row.event}-${String(row.data.callId ?? row.data.targetUserId ?? '')}`}
            size="small"
            pagination={{ pageSize: 8, hideOnSinglePage: true }}
            dataSource={qcEvents}
            locale={{ emptyText: qcWsLive ? '等待坐席动作事件…' : '质检流尚未连接' }}
            columns={[
              { title: '时间', width: 90, render: (_, row) => new Date(row.ts).toLocaleTimeString() },
              { title: '事件', dataIndex: 'event', width: 110, render: (v) => <Tag color={v === 'FORCE_LOGOUT' ? 'red' : v === 'RESULT' ? 'green' : 'blue'}>{String(v)}</Tag> },
              { title: '坐席', render: (_, row) => String(row.data.agentNo ?? row.data.targetAgentNo ?? '-') },
              { title: '话务/样本', render: (_, row) => `${row.data.callId != null ? `话务 #${row.data.callId}` : '-'}${row.data.sampleId != null ? ` / 样本 #${row.data.sampleId}` : ''}` },
              { title: '详情', render: (_, row) => row.data.detail != null ? String(row.data.detail) : row.data.releasedSamples != null ? `回池 ${row.data.releasedSamples}，注销 ${row.data.sessions ?? 0}` : '-' },
            ]}
          />
        </Card>
      )}

      <Card title="话务流水（最近 12 条）">
        <Table<CallRow>
          rowKey="id"
          size="small"
          pagination={false}
          dataSource={calls}
          columns={[
            { title: '话务', dataIndex: 'id', width: 70 },
            { title: '客户', dataIndex: 'cust_name', render: (v) => String(v ?? '-') },
            { title: '坐席', dataIndex: 'agent_no' },
            { title: '状态', dataIndex: 'status', render: (v) => <Tag>{String(v)}</Tag> },
            { title: '结果码', dataIndex: 'result_code', render: (v) => (v ? <Tag color="blue">{String(v)}</Tag> : '-') },
            { title: '开始', dataIndex: 'begin_time', render: (v) => String(v ?? '').slice(11, 19) },
            { title: '接通', dataIndex: 'connect_time', render: (v) => (v ? String(v).slice(11, 19) : '-') },
            {
              title: '录音', dataIndex: 'record_file', width: 60,
              render: (v, row) => (v ? <a href={`/api/recording/${row.id}`} target="_blank" rel="noreferrer">▶</a> : '-'),
            },
          ]}
        />
      </Card>

      {canAudit && (
        <Card title="答卷审核（督导 · 仅 SUBMITTED 可审）">
          <Table<SheetRow>
            rowKey="id"
            size="small"
            pagination={false}
            dataSource={sheets}
            locale={{ emptyText: '暂无待审答卷（坐席提交 SUCCESS/PARTIAL 后出现）' }}
            columns={[
              { title: '答卷', dataIndex: 'id', width: 70 },
              { title: '样本', dataIndex: 'sample_id', width: 70 },
              { title: '项目', dataIndex: 'project_id', width: 70 },
              { title: '问卷版本', dataIndex: 'qnr_version' },
              { title: '状态', dataIndex: 'status', render: (v) => <Tag color="orange">{String(v)}</Tag> },
              {
                title: '操作', width: 180,
                render: (_, row) => (
                  <Space>
                    <Button size="small" type="primary" onClick={() => setAuditTarget({ row, action: 'PASS' })}>通过</Button>
                    <Button size="small" danger onClick={() => setAuditTarget({ row, action: 'REJECT' })}>驳回</Button>
                  </Space>
                ),
              },
            ]}
          />
        </Card>
      )}

      <Modal
        open={!!auditTarget}
        title={auditTarget ? `${auditTarget.action === 'PASS' ? '通过' : '驳回'}答卷 #${auditTarget.row.id}` : ''}
        onCancel={() => setAuditTarget(null)}
        onOk={() => {
          const v = (document.getElementById('audit-remark') as HTMLInputElement | null)?.value ?? ''
          doAudit(v)
        }}
        okText="确认"
        okButtonProps={{ danger: auditTarget?.action === 'REJECT' }}
      >
        {auditTarget?.action === 'REJECT' && (
          <Typography.Paragraph type="warning" style={{ fontSize: 12 }}>
            驳回（REJECT）：SUCCESS 样本将回 IDLE 池重新派发，答卷置 REJECTED。
          </Typography.Paragraph>
        )}
        <Input id="audit-remark" placeholder="质检备注（可空）" style={{ marginTop: 8 }} />
      </Modal>
    </Space>
  )
}
