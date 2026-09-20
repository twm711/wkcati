import { useCallback, useEffect, useState } from 'react'
import { Card, Table, Tag, Button, Space, Drawer, Descriptions, App, Modal, Input, Select, InputNumber, Typography, Statistic, Form, Popconfirm } from 'antd'
import { PlusOutlined } from '@ant-design/icons'
import { api, hasRole, rawSession, type RInfo } from '../api'

interface Proj {
  projectId: number; projectCode: string; name: string; status: string
  questionnaireId: number; qnrVersion: string; qnrStatus: string
  samples: { total: number; idle: number }; quota: { done: number; target: number }
}
interface Opt { optionId: number; text: string }
interface QD { questionId: number; qNo: number; type: string; title: string; min: unknown; max: unknown; options: Opt[] }
interface Detail {
  projectId: number; name: string; status: string; questionnaireId: number
  qnrVersion: string; qnrStatus: string
  questions: QD[]
  quotaCells: { cellId: number; conditions: { questionId: number; in: number[] }[]; target: number; done: number }[]
  samplesByStatus: Record<string, number>
  sheetVersions: Record<string, number>
}
interface Rep { questionId: number; title: string; total: number; items: { optionId: number; label: string; count: number; pct: number }[] }

const ST_COLOR: Record<string, string> = { DRAFT: 'default', RUNNING: 'processing', PAUSED: 'warning', FINISHED: 'green' }
const QT_LABEL: Record<string, string> = { single: '单选', multi: '多选', number: '数值', text: '文本' }

export default function Projects() {
  const { message } = App.useApp()
  const [rows, setRows] = useState<Proj[]>([])
  const [detail, setDetail] = useState<Detail | null>(null)
  const [rep, setRep] = useState<Rep | null>(null)
  const [newOpen, setNewOpen] = useState(false)
  const [newName, setNewName] = useState('')
  const [impOpen, setImpOpen] = useState(false)
  const [names, setNames] = useState('')
  const [phones, setPhones] = useState('')
  const [qOpen, setQOpen] = useState(false)
  const [qForm, setQForm] = useState({ text: '', qType: 'single', options: '满意,不满意' })
  const [quotaOpen, setQuotaOpen] = useState(false)
  const [quotaForm, setQuotaForm] = useState<{ questionId: number | undefined; inList: number[]; target: number }>({ questionId: undefined, inList: [], target: 2 })
  const canManage = hasRole('groupAdmin')

  const load = useCallback(async () => {
    const r = await api.get<Proj[]>('/api/project')
    if (r.success) setRows(r.data as unknown as Proj[])
  }, [])
  useEffect(() => {
    load()
  }, [load])

  const openDetail = async (pid: number) => {
    const r = await api.get<Detail>(`/api/project/${pid}`)
    if (r.success) setDetail(r.data)
  }

  const run = async (p: Promise<RInfo<unknown>>, ok?: (d: Record<string, unknown>) => void) => {
    try {
      const r = await p
      if (!r.success) throw new Error(r.message)
      message.success(r.message)
      ok?.(r.data as Record<string, unknown>)
      setDetail(null)
      load()
    } catch (e) {
      message.error(e instanceof Error ? e.message : '操作失败')
    }
  }

  const addQuestion = () =>
    run(
      api.post(`/api/project/${detail!.projectId}/questions`, {
        text: qForm.text, qType: qForm.qType,
        options: qForm.qType === 'single' || qForm.qType === 'multi' ? qForm.options.split(/[,，]/).map((s) => s.trim()).filter(Boolean) : undefined,
      }),
    ).then(() => {
      setQOpen(false)
      setQForm({ text: '', qType: 'single', options: '满意,不满意' })
      openDetail(detail!.projectId)
    })

  const revise = () =>
    run(api.post(`/api/project/${detail!.projectId}/revise`, {
      addQuestions: [{ text: qForm.text || '补充题', qType: qForm.qType === 'text' ? 'text' : 'single', options: qForm.qType === 'single' ? qForm.options.split(/[,，]/).map((s) => s.trim()).filter(Boolean) : undefined }],
    })).then(() => {
      setQOpen(false)
      openDetail(detail!.projectId)
    })

  const importSamples = () => {
    const ns = names.split('\n').map((s) => s.trim()).filter(Boolean)
    const ps = phones.split('\n').map((s) => s.trim()).filter(Boolean)
    if (ns.length === 0 || ns.length !== ps.length) {
      message.warning('名单与号码需逐行对应且非空')
      return
    }
    run(api.post(`/api/project/${detail!.projectId}/samples`, { names: ns, phones: ps })).then(() => {
      setImpOpen(false)
      setNames('')
      setPhones('')
      openDetail(detail!.projectId)
    })
  }

  const setQuota = () =>
    run(
      api.put(`/api/project/${detail!.projectId}/quota`, {
        name: '配额-Web',
        cells: [{ questionId: quotaForm.questionId, inList: quotaForm.inList, target: quotaForm.target }],
      }),
    ).then(() => {
      setQuotaOpen(false)
      openDetail(detail!.projectId)
    })

  const showRep = async (questionId: number) => {
    const r = await api.get<Rep>(`/api/report/single?questionId=${questionId}`)
    if (r.success) setRep(r.data)
  }

  return (
    <Card
      title="项目管理（多项目 / 多问卷 / 版本化修订 / 配额）"
      extra={
        canManage && (
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setNewOpen(true)}>
            新建项目
          </Button>
        )
      }
    >
      <Table<Proj>
        rowKey="projectId"
        size="small"
        pagination={false}
        dataSource={rows}
        columns={[
          { title: 'ID', dataIndex: 'projectId', width: 50 },
          { title: '编号', dataIndex: 'projectCode', width: 100 },
          { title: '名称', dataIndex: 'name' },
          {
            title: '状态', dataIndex: 'status', width: 90,
            render: (v) => <Tag color={ST_COLOR[v] ?? 'default'}>{v}</Tag>,
          },
          { title: '问卷', dataIndex: 'qnrVersion', render: (v, row) => `${v}（${row.qnrStatus}）` },
          { title: '样本', render: (_, r) => `${r.samples.idle} 可派 / ${r.samples.total} 总量` },
          {
            title: '配额', render: (_, r) =>
              r.quota.target > 0 ? `${r.quota.done}/${r.quota.target}` : '-',
          },
          {
            title: '操作', width: 120,
            render: (_, r) => <Button size="small" onClick={() => openDetail(r.projectId)}>详情</Button>,
          },
        ]}
      />

      <Drawer open={!!detail} width={560} onClose={() => setDetail(null)} title={detail ? `项目 #${detail.projectId} ${detail.name}` : ''}>
        {detail && (
          <Space direction="vertical" size={12} style={{ width: '100%' }}>
            <Descriptions bordered size="small" column={2}
              items={[
                { key: 's', label: '状态', children: <Tag color={ST_COLOR[detail.status]}>{detail.status}</Tag> },
                { key: 'v', label: '问卷版本', children: `${detail.qnrVersion}（${detail.qnrStatus}）` },
                { key: 'q', label: '问卷 ID', children: detail.questionnaireId },
                {
                  key: 'sv', label: '答卷版本分布',
                  children: Object.entries(detail.sheetVersions).map(([k, v]) => (
                    <Tag key={k}>{k} × {v}</Tag>
                  )),
                },
              ]}
            />
            <Space wrap>
              {canManage && detail.status === 'DRAFT' && (
                <Popconfirm title="发布后问卷冻结，确认发布？" onConfirm={() => run(api.post(`/api/project/${detail.projectId}/publish`))}>
                  <Button type="primary">发布</Button>
                </Popconfirm>
              )}
              {canManage && detail.qnrStatus === 'PUBLISHED' && (
                <Button onClick={() => { setQForm({ text: '', qType: 'single', options: '满意,不满意' }); setQOpen(true) }}>
                  修订加题（v +0.1）
                </Button>
              )}
              {canManage && detail.status === 'RUNNING' && (
                <>
                  <Button onClick={() => run(api.post(`/api/project/${detail.projectId}/status`, { action: 'pause' }))}>暂停</Button>
                  <Popconfirm title="结项为终态不可恢复，确认？" onConfirm={() => run(api.post(`/api/project/${detail.projectId}/status`, { action: 'finish' }))}>
                    <Button danger>结项</Button>
                  </Popconfirm>
                </>
              )}
              {canManage && detail.status === 'PAUSED' && (
                <Button type="primary" onClick={() => run(api.post(`/api/project/${detail.projectId}/status`, { action: 'resume' }))}>恢复运行</Button>
              )}
              {canManage && (
                <>
                  <Button onClick={() => setImpOpen(true)}>导入样本</Button>
                  <Button onClick={() => setQuotaOpen(true)}>设置配额</Button>
                </>
              )}
              <ExportButtons projectId={detail.projectId} />
            </Space>
            <Card size="small" title={`题目（${detail.questions.length}）`}>
              {detail.questions.map((q) => (
                <Card key={q.questionId} size="small" style={{ marginBottom: 8 }}
                  title={<span>Q{q.qNo} {q.title} <Tag>{QT_LABEL[q.type] ?? q.type}</Tag></span>}
                  extra={<Button size="small" type="link" onClick={() => showRep(q.questionId)}>统计</Button>}
                >
                  <Space wrap>
                    {q.options.map((o) => (
                      <Tag key={o.optionId}>{o.optionId}:{o.text}</Tag>
                    ))}
                    {q.type === 'number' && <Typography.Text type="secondary">范围 {String(q.min ?? '-')} ~ {String(q.max ?? '-')}</Typography.Text>}
                  </Space>
                </Card>
              ))}
            </Card>
            {detail.quotaCells.length > 0 && (
              <Card size="small" title="配额单元">
                {detail.quotaCells.map((c) => (
                  <div key={c.cellId} style={{ marginBottom: 6 }}>
                    <Space>
                      <Typography.Text style={{ fontSize: 12 }}>
                        {c.conditions.map((cd) => `题${cd.questionId}∈{${cd.in.join(',')}}`).join(' 且 ')}
                      </Typography.Text>
                      <Statistic value={c.done} suffix={`/ ${c.target}`} valueStyle={{ fontSize: 13 }} />
                    </Space>
                  </div>
                ))}
              </Card>
            )}
            <Card size="small" title="样本状态分布">
              <Space wrap>
                {Object.entries(detail.samplesByStatus).map(([k, v]) => (
                  <Tag key={k} color={k === 'IDLE' ? 'green' : k.startsWith('CLOSED') ? 'blue' : 'default'}>{k} × {v}</Tag>
                ))}
              </Space>
            </Card>
          </Space>
        )}
      </Drawer>

      <Modal open={newOpen} title="新建项目（草稿）" onCancel={() => setNewOpen(false)} onOk={() => run(api.post('/api/project', { name: newName })).then(() => { setNewOpen(false); setNewName('') })} okText="创建">
        <Input value={newName} onChange={(e) => setNewName(e.target.value)} placeholder="项目名称" />
      </Modal>
      <Modal open={impOpen} title={`导入样本 → 项目 #${detail?.projectId ?? ''}（项目内号码查重）`} onCancel={() => setImpOpen(false)} onOk={importSamples} okText="导入">
        <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>名单与号码逐行对应（半年原则/黑名单在派样端过滤）</Typography.Paragraph>
        <Input.TextArea rows={4} value={names} onChange={(e) => setNames(e.target.value)} placeholder={'姓名甲\n姓名乙'} style={{ marginBottom: 8 }} />
        <Input.TextArea rows={4} value={phones} onChange={(e) => setPhones(e.target.value)} placeholder={'13800000101\n13800000102'} />
      </Modal>
      <Modal
        open={qOpen}
        title={detail?.qnrStatus === 'PUBLISHED' ? '修订加题（生成新版本）' : '新增题目（草稿期）'}
        onCancel={() => setQOpen(false)}
        onOk={() => (detail?.qnrStatus === 'PUBLISHED' ? revise() : addQuestion())}
        okText="提交"
      >
        <Form layout="vertical">
          <Form.Item label="题干">
            <Input value={qForm.text} onChange={(e) => setQForm({ ...qForm, text: e.target.value })} />
          </Form.Item>
          <Form.Item label="题型">
            <Select value={qForm.qType} onChange={(v) => setQForm({ ...qForm, qType: v })}
              options={[{ value: 'single', label: '单选' }, { value: 'multi', label: '多选' }, { value: 'number', label: '数值' }, { value: 'text', label: '文本' }]} />
          </Form.Item>
          {(qForm.qType === 'single' || qForm.qType === 'multi') && (
            <Form.Item label="选项（逗号分隔）">
              <Input value={qForm.options} onChange={(e) => setQForm({ ...qForm, options: e.target.value })} />
            </Form.Item>
          )}
        </Form>
      </Modal>
      <Modal open={quotaOpen} title="设置配额（原子扣减 done<target）" onCancel={() => setQuotaOpen(false)} onOk={setQuota} okText="保存">
        <Form layout="vertical">
          <Form.Item label="配额题目">
            <Select
              value={quotaForm.questionId}
              onChange={(v) => setQuotaForm({ ...quotaForm, questionId: v, inList: [] })}
              placeholder="选择题目"
              options={(detail?.questions ?? []).filter((q) => q.options.length > 0).map((q) => ({ value: q.questionId, label: `Q${q.qNo} ${q.title}` }))}
            />
          </Form.Item>
          <Form.Item label="命中选项">
            <Select
              mode="multiple"
              value={quotaForm.inList}
              onChange={(v) => setQuotaForm({ ...quotaForm, inList: v })}
              placeholder="选择选项"
              options={(detail?.questions ?? []).find((q) => q.questionId === quotaForm.questionId)?.options.map((o) => ({ value: o.optionId, label: o.text })) ?? []}
            />
          </Form.Item>
          <Form.Item label="目标份数">
            <InputNumber min={1} value={quotaForm.target} onChange={(v) => setQuotaForm({ ...quotaForm, target: v ?? 1 })} />
          </Form.Item>
        </Form>
      </Modal>
      <Modal open={!!rep} title={rep ? `单题统计：${rep.title}` : ''} onCancel={() => setRep(null)} footer={null}>
        {rep && (
          <>
            <Statistic title="有效答卷" value={rep.total} style={{ marginBottom: 12 }} />
            <Table
              rowKey="optionId"
              size="small"
              pagination={false}
              dataSource={rep.items}
              columns={[
                { title: '选项', dataIndex: 'label' },
                { title: '计数', dataIndex: 'count', width: 80 },
                { title: '占比', dataIndex: 'pct', width: 100, render: (v) => `${Number(v).toFixed(1)}%` },
              ]}
            />
          </>
        )}
      </Modal>
    </Card>
  )
}

// 导出中心按钮：?token= 直链下载（window.open 无法带 Authorization 头）
export function ExportButtons({ projectId }: { projectId: number }) {
  const { message } = App.useApp()
  const dl = (fmt: 'csv' | 'xlsx' | 'sav') => {
    const s = rawSession()
    if (!s) { message.error('会话已过期，请重新登录'); return }
    window.open(`/api/export/${projectId}/sheets.${fmt}?token=${encodeURIComponent(s.sessionId)}`, '_blank')
  }
  return (
    <Space.Compact>
      <Button onClick={() => dl('csv')}>导出 CSV</Button>
      <Button onClick={() => dl('xlsx')}>XLSX</Button>
      <Button onClick={() => dl('sav')}>SPSS</Button>
    </Space.Compact>
  )
}
