import { useEffect, useState } from 'react'
import { Card, Button, Descriptions, Tag, Radio, InputNumber, Input, Select, App, Space, Alert, Typography, Empty, Spin } from 'antd'
import { PhoneOutlined, CheckCircleOutlined } from '@ant-design/icons'
import { api, rawSession } from '../api'

// 与 Go 后端种子一致的 10 个结果码（出处：001_init.sql + demo 行为基线）
const RESULT_CODES = [
  { v: 'SUCCESS', label: 'SUCCESS 完成', hitBlack: false },
  { v: 'PARTIAL', label: 'PARTIAL 部分完成', hitBlack: false },
  { v: 'QUFAIL', label: 'QUFAIL 配额满', hitBlack: false },
  { v: 'REFUSE', label: 'REFUSE 拒访', hitBlack: true },
  { v: 'BREAKOFF', label: 'BREAKOFF 中断', hitBlack: false },
  { v: 'APPOINT', label: 'APPOINT 预约', hitBlack: false },
  { v: 'NA', label: 'NA 无应答', hitBlack: false },
  { v: 'BUSY', label: 'BUSY 占线', hitBlack: false },
  { v: 'INVALID', label: 'INVALID 无效号码', hitBlack: true },
  { v: 'FAX', label: 'FAX 传真', hitBlack: false },
]

interface Opt { optionId: number; text: string; value: string }
interface Q { questionId: number; qNo: number; type: string; title: string; required: boolean; min: number | null; max: number | null; options: Opt[] }
interface Dispatch {
  callId: number; sampleId: number; custName: string; attempts: number
  phones: string[]; currentPhone: string
  questionnaire: { questionnaireId: number; title: string; version: string; questions: Q[] }
}
interface Answered { [questionId: number]: string }
interface Preview { taskId: number; sampleId: number; custName: string; phone: string; previewSeconds: number }

export default function Agent() {
  const { message } = App.useApp()
  const [loading, setLoading] = useState(false)
  const [disp, setDisp] = useState<Dispatch | null>(null)
  const [preview, setPreview] = useState<Preview | null>(null)
  const [previewLeft, setPreviewLeft] = useState(0)
  const [answered, setAnswered] = useState<Answered>({})
  const [values, setValues] = useState<Record<number, { optionIds?: number[]; numericValue?: number | null; answerText?: string }>>({})
  const [resultCode, setResultCode] = useState<string>('SUCCESS')
  const [lastResult, setLastResult] = useState<Record<string, unknown> | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [agentState, setAgentState] = useState<'READY' | 'BUSY' | 'PAUSE'>('READY')

  useEffect(() => {
    const s = rawSession(); if (!s) return
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    let closed = false
    let retry: ReturnType<typeof setTimeout> | null = null
    let ws: WebSocket | null = null
    const connect = () => {
      if (closed) return
      ws = new WebSocket(`${proto}://${location.host}/api/agent/ws?token=${encodeURIComponent(s.sessionId)}`)
      ws.onmessage = (ev) => { try { const f = JSON.parse(ev.data as string) as { type: string; text?: string }; if (f.type === 'message') setNotice(f.text ?? '') } catch { /* ignore */ } }
      ws.onclose = () => { if (!closed) retry = setTimeout(connect, 5000) }
    }
    connect()
    return () => { closed = true; if (retry) clearTimeout(retry); ws?.close() }
  }, [])

  useEffect(() => {
    if (!disp) return
    const renew = async () => {
      const r = await api.post('/api/agent/task/renew', { callId: disp.callId })
      if (!r.success) message.warning(`任务租约续期失败：${r.message}`)
    }
    const timer = window.setInterval(renew, 120000)
    return () => window.clearInterval(timer)
  }, [disp, message])

  const changeState = async (state: 'READY' | 'BUSY' | 'PAUSE') => {
    const r = await api.post('/api/agent/state', { state })
    if (!r.success) { message.error(r.message); return }
    setAgentState(state); message.success(`坐席状态：${state}`)
  }

  const dispatch = async () => {
    setLoading(true)
    setLastResult(null)
    try {
      const r = await api.get<Dispatch | null>('/api/agent/dispatch?projectId=1')
      if (r.success && r.data) {
        setDisp(r.data)
        setAnswered({})
        setValues({})
        message.success(`已派样：${r.data.custName}（第 ${r.data.attempts + 1} 次触达）`)
      } else {
        setDisp(null)
        message.warning(r.message)
      }
    } finally {
      setLoading(false)
    }
  }

  const getPreview = async () => {
    setLoading(true)
    const r = await api.get<Preview>('/api/agent/preview?projectId=1')
    setLoading(false)
    if (!r.success || !r.data) { message.warning(r.message); return }
    setPreview(r.data); setPreviewLeft(r.data.previewSeconds)
  }

  useEffect(() => {
    if (!preview) return
    const timer = window.setInterval(() => setPreviewLeft((v) => {
      if (v <= 1) { void api.post(`/api/agent/preview/${preview.taskId}/skip`); setPreview(null); message.info('预览已超时并回池'); return 0 }
      return v - 1
    }), 1000)
    return () => window.clearInterval(timer)
  }, [preview, message])

  const confirmPreview = async () => {
    if (!preview) return
    const r = await api.post<{ callId:number; sampleId:number }>(`/api/agent/preview/${preview.taskId}/confirm`)
    if (!r.success) { message.error(r.message); return }
    setPreview(null)
    message.success(`${r.message}，话务 #${r.data.callId} 已建立；请继续使用外呼媒体或刷新工作台获取问卷`)
  }
  const skipPreview = async () => { if (!preview) return; const r = await api.post(`/api/agent/preview/${preview.taskId}/skip`); if (r.success) { setPreview(null); message.success(r.message) } else message.error(r.message) }

  const submitAnswer = async (q: Q) => {
    const v = values[q.questionId] ?? {}
    try {
      const r = await api.post<{ sheetId: number; answeredAt: string }>('/api/agent/answer', {
        callId: disp!.callId,
        questionId: q.questionId,
        optionIds: v.optionIds,
        numericValue: v.numericValue ?? undefined,
        answerText: v.answerText,
      })
      if (!r.success) throw new Error(r.message)
      setAnswered((a) => ({ ...a, [q.questionId]: r.data.answeredAt }))
      message.success(`题 ${q.qNo} 已实时入库（sheet #${r.data.sheetId}）`)
    } catch (e) {
      message.error(e instanceof Error ? e.message : '提交失败')
    }
  }

  const submitResult = async () => {
    try {
      const r = await api.post<Record<string, unknown>>('/api/agent/result', { callId: disp!.callId, resultCode })
      if (!r.success) throw new Error(r.message)
      setLastResult({ ...r.data, code: resultCode })
      setDisp(null)
      setAnswered({})
    } catch (e) {
      message.error(e instanceof Error ? e.message : '提交失败')
    }
  }

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      {notice && <Alert message="督导消息" description={notice} type="info" closable onClose={() => setNotice(null)} style={{ width: '100%' }} />}
      <Card size="small" title="坐席状态">
        <Space>
          <Tag color={agentState === 'READY' ? 'green' : agentState === 'BUSY' ? 'orange' : 'default'}>{agentState}</Tag>
          <Button size="small" type={agentState === 'READY' ? 'primary' : 'default'} onClick={() => changeState('READY')} disabled={!!disp}>示闲</Button>
          <Button size="small" onClick={() => changeState('BUSY')} disabled={!!disp}>示忙</Button>
          <Button size="small" onClick={() => changeState('PAUSE')} disabled={!!disp}>小休</Button>
        </Space>
      </Card>
      <Card
        title={<span><PhoneOutlined /> 坐席工作台（外呼作答）</span>}
        extra={
          <Space>
            <Button onClick={getPreview} loading={loading} disabled={!!disp || !!preview}>预览样本</Button>
            <Button type="primary" loading={loading} onClick={dispatch} disabled={!!disp || !!preview}>
              {disp ? '话务进行中…' : '① 获取派样（项目 1）'}
            </Button>
          </Space>
        }
      >
        {!disp && !preview && !lastResult && <Empty description="点击「获取派样」开始外呼，或先锁定预览样本" />}
        {preview && <Alert type="warning" showIcon message={`预览：${preview.custName} / ${preview.phone}`} description={`剩余 ${previewLeft} 秒；确认后建立话务，跳过则样本回池。`} action={<Space><Button size="small" type="primary" onClick={confirmPreview}>确认拨打</Button><Button size="small" onClick={skipPreview}>跳过</Button></Space>} />}
        {lastResult && (
          <Alert
            type="success"
            showIcon
            icon={<CheckCircleOutlined />}
            message={`结果码 ${lastResult.code} 已提交`}
            description={
              <pre style={{ margin: 0, fontSize: 12, whiteSpace: 'pre-wrap' }}>{JSON.stringify(lastResult, null, 2)}</pre>
            }
          />
        )}
        {disp && (
          <>
            <Descriptions
              size="small"
              bordered
              column={3}
              items={[
                { key: 'n', label: '客户', children: disp.custName },
                { key: 'p', label: '当前号码', children: <Typography.Text copyable>{disp.currentPhone}</Typography.Text> },
                { key: 'a', label: '重拨次数', children: disp.attempts },
                { key: 's', label: '样本 ID', children: `#${disp.sampleId}` },
                { key: 'c', label: '话务 ID', children: `#${disp.callId}` },
                { key: 'ph', label: '全部号码', children: disp.phones.join(' / ') },
              ]}
            />
            <Typography.Title level={5} style={{ margin: '16px 0 8px' }}>
              问卷：{disp.questionnaire.title}（{disp.questionnaire.version}）· 作答实时落库
            </Typography.Title>
            {disp.questionnaire.questions.map((q) => (
              <Card key={q.questionId} size="small" style={{ marginBottom: 10 }} title={`Q${q.qNo} ${q.title}${q.required ? ' *' : ''}`}>
                <Space direction="vertical" style={{ width: '100%' }}>
                  {q.type === 'single' && (
                    <Radio.Group
                      onChange={(e) => setValues((v) => ({ ...v, [q.questionId]: { optionIds: [e.target.value] } }))}
                      disabled={!!answered[q.questionId]}
                    >
                      {q.options.map((o) => (
                        <Radio key={o.optionId} value={o.optionId}>
                          {o.text} <Tag style={{ marginLeft: 4 }}>{o.optionId}</Tag>
                        </Radio>
                      ))}
                    </Radio.Group>
                  )}
                  {(q.type === 'multi') && (
                    <Select
                      mode="multiple"
                      style={{ width: 300 }}
                      placeholder="选择选项"
                      disabled={!!answered[q.questionId]}
                      onChange={(vs: number[]) => setValues((v) => ({ ...v, [q.questionId]: { optionIds: vs } }))}
                      options={q.options.map((o) => ({ value: o.optionId, label: o.text }))}
                    />
                  )}
                  {q.type === 'number' && (
                    <Space>
                      <InputNumber
                        min={q.min ?? undefined}
                        max={q.max ?? undefined}
                        disabled={!!answered[q.questionId]}
                        onChange={(v) => setValues((val) => ({ ...val, [q.questionId]: { numericValue: v } }))}
                      />
                      <Typography.Text type="secondary">范围 {q.min ?? '-∞'} ~ {q.max ?? '+∞'}（后端兜底校验）</Typography.Text>
                    </Space>
                  )}
                  {q.type === 'text' && (
                    <Input.TextArea
                      rows={2}
                      disabled={!!answered[q.questionId]}
                      onChange={(e) => setValues((v) => ({ ...v, [q.questionId]: { answerText: e.target.value } }))}
                    />
                  )}
                  <Space>
                    <Button
                      size="small"
                      type="primary"
                      ghost
                      disabled={!!answered[q.questionId] || !values[q.questionId]}
                      onClick={() => submitAnswer(q)}
                    >
                      提交本题
                    </Button>
                    {answered[q.questionId] && (
                      <Tag color="green">已入库 {answered[q.questionId].slice(11, 19)}</Tag>
                    )}
                  </Space>
                </Space>
              </Card>
            ))}
            <Card size="small" title="② 话务结果（去向引擎 + 配额扣减 + 幂等）">
              <Space wrap>
                <Select style={{ width: 220 }} value={resultCode} onChange={setResultCode}
                  options={RESULT_CODES.map((c) => ({ value: c.v, label: c.label }))} />
                <Button type="primary" danger={!!RESULT_CODES.find((c) => c.v === resultCode)?.hitBlack} onClick={submitResult}>
                  提交结果
                </Button>
                <Typography.Text type="secondary">
                  REFUSE/INVALID 自动进黑名单；APPOINT→预约池；NA/BUSY→回池重拨；SUCCESS→闭环+配额
                </Typography.Text>
              </Space>
            </Card>
          </>
        )}
      </Card>
      <Spin tip="加载中" spinning={false} />
    </Space>
  )
}
