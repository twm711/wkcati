import { useCallback, useEffect, useState } from 'react'
import { Card, Col, Row, Tag, Button, Space, Input, Timeline, Descriptions, Table, App, Typography, Empty } from 'antd'
import { AudioOutlined, PhoneOutlined } from '@ant-design/icons'
import { api } from '../api'

interface FlowNode {
  id: string; type: string; text: string; next: string
  branches?: Record<string, string>; options?: Record<string, string>
  tag?: string; queue?: string
}
interface Flow { name: string; flow: { entry: string; nodes: FlowNode[] }; updatedAt: string }
interface IvrSession {
  sessionId: string; callerNo: string; node: FlowNode
  transcript: Record<string, unknown>[]; answers: Record<string, string>
  done: boolean; outcome: string; invalidKey?: boolean
}
interface LogRow { id: number; caller_no: string; start_time: string; end_time: unknown; outcome: string; path_json: string; answers_json: string }

const TYPE_COLOR: Record<string, string> = {
  play: 'default', menu: 'processing', question: 'geekblue', voicemail: 'purple', transfer: 'orange', end: 'green',
}

export default function Ivr() {
  const { message } = App.useApp()
  const [flow, setFlow] = useState<Flow | null>(null)
  const [sess, setSess] = useState<IvrSession | null>(null)
  const [callerNo, setCallerNo] = useState('')
  const [logs, setLogs] = useState<LogRow[]>([])
  const [vmText, setVmText] = useState('留言：请回电')

  const load = useCallback(async () => {
    const f = await api.get<Flow>('/api/ivr/flow')
    if (f.success) setFlow(f.data)
    const l = await api.get<LogRow[]>('/api/ivr/logs')
    if (l.success) setLogs(l.data as unknown as LogRow[])
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const start = async () => {
    const r = await api.post<IvrSession>('/api/ivr/call', callerNo ? { callerNo } : {})
    if (!r.success) {
      message.error(r.message)
      return
    }
    setSess(r.data)
    if (r.data.done) message.info(`结局：${r.data.outcome}`)
  }

  const press = async (key: string) => {
    if (!sess) return
    const body: Record<string, unknown> =
      sess.node.type === 'voicemail' && key === '#' ? { key: '#', message: vmText } : { key }
    const r = await api.post<IvrSession>(`/api/ivr/call/${sess.sessionId}/input`, body)
    if (!r.success) {
      message.error(r.message)
      return
    }
    setSess((s) => (s ? { ...r.data, sessionId: s.sessionId, callerNo: s.callerNo } : r.data))
    if (r.data.invalidKey) message.warning('无效按键，已重播当前节点')
    if (r.data.outcome) message.info(`结局：${r.data.outcome}${r.data.outcome.startsWith('TRANSFER') ? '（已自动落工单）' : ''}`)
    if (r.data.done || r.data.outcome) load()
  }

  const hangup = async () => {
    if (!sess) return
    const r = await api.post<IvrSession>(`/api/ivr/call/${sess.sessionId}/hangup`)
    if (r.success) {
      message.info(`主叫挂断，结局：${r.data.outcome}，话务已落库`)
      setSess(null)
      load()
    }
  }

  const node = sess?.node
  const keyHints: [string, string][] = node
    ? Object.entries(node.branches ?? node.options ?? {}).map(([k, v]) =>
        node.type === 'question' ? [k, `${k} → ${v}`] : [k, `${k} → 节点 ${v}`],
      )
    : []

  return (
    <Row gutter={16}>
      <Col span={10}>
        <Card title={<span><AudioOutlined /> IVR 流程（{flow?.name ?? ''}）</span>} size="small">
          {flow ? (
            <>
              <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
                入口 {flow.flow.entry} · 更新于 {String(flow.updatedAt).slice(0, 19).replace('T', ' ')}
                （设计器可视化编排见下一里程碑；当前为解释器只读视图）
              </Typography.Paragraph>
              <Timeline
                items={flow.flow.nodes.map((n) => ({
                  children: (
                    <div>
                      <Space wrap>
                        <Tag color={TYPE_COLOR[n.type] ?? 'default'}>{n.id} · {n.type}</Tag>
                        <Typography.Text style={{ fontSize: 12 }}>{n.text}</Typography.Text>
                      </Space>
                      {n.branches && (
                        <div style={{ fontSize: 11, color: '#888' }}>
                          {Object.entries(n.branches).map(([k, v]) => `${k}键→${v}`).join('；')}
                        </div>
                      )}
                      {n.queue && <div style={{ fontSize: 11, color: '#888' }}>队列 {n.queue}</div>}
                    </div>
                  ),
                }))}
              />
            </>
          ) : (
            <Empty />
          )}
        </Card>
      </Col>
      <Col span={14}>
        <Space direction="vertical" size={16} style={{ width: '100%' }}>
          <Card
            title={<span><PhoneOutlined /> 话机模拟器</span>}
            extra={
              <Space>
                <Input
                  style={{ width: 150 }}
                  placeholder="主叫号码（可空）"
                  value={callerNo}
                  onChange={(e) => setCallerNo(e.target.value)}
                  disabled={!!sess}
                />
                <Button type="primary" onClick={start} disabled={!!sess}>呼入</Button>
                <Button danger onClick={hangup} disabled={!sess}>挂断</Button>
              </Space>
            }
          >
            {!sess && <Empty description="输入主叫号码（或留空自动生成）后点击「呼入」，走线与真机 IVR 解释器一致" />}
            {sess && (
              <Space direction="vertical" size={8} style={{ width: '100%' }}>
                <Descriptions size="small" column={3} bordered
                  items={[
                    { key: 's', label: '会话', children: sess.sessionId.slice(0, 10) + '…' },
                    { key: 'c', label: '主叫', children: sess.callerNo },
                    { key: 'o', label: '结局', children: sess.outcome ? <Tag color="green">{sess.outcome}</Tag> : '进行中' },
                  ]}
                />
                <Card size="small" style={{ background: '#f6ffed' }} title={<span>当前节点 <Tag color={TYPE_COLOR[node?.type ?? '']}>{node?.id} · {node?.type}</Tag></span>}>
                  <Typography.Paragraph style={{ marginBottom: node ? 8 : 0 }}>{node?.text}</Typography.Paragraph>
                  {node?.type === 'voicemail' && (
                    <Space style={{ marginBottom: 8 }}>
                      <Input value={vmText} onChange={(e) => setVmText(e.target.value)} style={{ width: 260 }} />
                      <Button type="primary" size="small" onClick={() => press('#')}># 结束留言</Button>
                    </Space>
                  )}
                  {keyHints.length > 0 && (
                    <Space wrap>
                      {keyHints.map(([k, hint]) => (
                        <Button key={k} size="small" onClick={() => press(k)}>
                          {hint}
                        </Button>
                      ))}
                    </Space>
                  )}
                </Card>
                <Space wrap>
                  {['1', '2', '3', '4', '5', '6', '7', '8', '9', '0'].map((k) => (
                    <Button key={k} style={{ width: 44 }} onClick={() => press(k)}>{k}</Button>
                  ))}
                </Space>
                {sess.transcript.length > 0 && (
                  <Timeline
                    items={sess.transcript.map((t, i) => ({
                      key: i,
                      children: (
                        <span style={{ fontSize: 12 }}>
                          <Tag>{String(t.node)}</Tag> {String(t.text ?? '')} {t.event ? <Tag color="orange">{String(t.event)}</Tag> : null}
                        </span>
                      ),
                    }))}
                  />
                )}
                {Object.keys(sess.answers).length > 0 && (
                  <div>
                    {Object.entries(sess.answers).map(([k, v]) => (
                      <Tag key={k} color="blue">{k} = {v}</Tag>
                    ))}
                  </div>
                )}
              </Space>
            )}
          </Card>
          <Card title="呼入话务日志（最近 20 条）" size="small">
            <Table<LogRow>
              rowKey="id"
              size="small"
              pagination={{ pageSize: 5 }}
              dataSource={logs}
              columns={[
                { title: 'ID', dataIndex: 'id', width: 50 },
                { title: '主叫', dataIndex: 'caller_no' },
                { title: '开始', dataIndex: 'start_time', render: (v) => String(v).slice(11, 19) },
                { title: '结局', dataIndex: 'outcome', render: (v) => <Tag color={String(v).startsWith('TRANSFER') ? 'orange' : 'blue'}>{String(v)}</Tag> },
                { title: '路径', dataIndex: 'path_json', ellipsis: true, render: (v) => <Typography.Text style={{ fontSize: 11 }}>{String(v)}</Typography.Text> },
              ]}
            />
          </Card>
        </Space>
      </Col>
    </Row>
  )
}
