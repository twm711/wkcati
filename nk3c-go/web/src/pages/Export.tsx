import { useEffect, useState } from 'react'
import { Card, Select, Checkbox, Button, Space, Table, Tag, App, Typography, Divider } from 'antd'
import { DownloadOutlined, DeleteOutlined } from '@ant-design/icons'
import { api, rawSession } from '../api'

interface Project { projectId: number; name: string; status: string }
interface History { key: string; project: string; format: string; columns: number; at: string }

const HISTORY_KEY = 'nk3c_export_history'

export default function Export() {
  const { message } = App.useApp()
  const [projects, setProjects] = useState<Project[]>([])
  const [pid, setPid] = useState<number>()
  const [headers, setHeaders] = useState<string[]>([])
  const [selected, setSelected] = useState<number[]>([])
  const [format, setFormat] = useState('csv')
  const [history, setHistory] = useState<History[]>(() => {
    try { return JSON.parse(localStorage.getItem(HISTORY_KEY) ?? '[]') as History[] } catch { return [] }
  })

  useEffect(() => { api.get<Project[]>('/api/project').then((r) => { if (r.success) setProjects(r.data) }) }, [])
  useEffect(() => {
    if (!pid) { setHeaders([]); setSelected([]); return }
    api.get<{ headers: string[] }>(`/api/export/${pid}/columns`).then((r) => {
      if (r.success) { setHeaders(r.data.headers); setSelected(r.data.headers.map((_, i) => i)) }
      else message.error(r.message)
    })
  }, [pid, message])

  const download = () => {
    if (!pid || selected.length === 0) { message.warning('请选择项目和至少一列'); return }
    const s = rawSession()
    if (!s) return
    const p = projects.find((x) => x.projectId === pid)
    const url = `/api/export/${pid}/${format}?cols=${selected.join(',')}&token=${encodeURIComponent(s.sessionId)}`
    window.open(url, '_blank', 'noopener,noreferrer')
    const item: History = { key: `${Date.now()}`, project: p?.name ?? `#${pid}`, format: format.toUpperCase(), columns: selected.length, at: new Date().toLocaleString() }
    const next = [item, ...history].slice(0, 20)
    setHistory(next); localStorage.setItem(HISTORY_KEY, JSON.stringify(next))
  }

  const clearHistory = () => { setHistory([]); localStorage.removeItem(HISTORY_KEY) }

  return <Space direction="vertical" size={16} style={{ width: '100%' }}>
    <Card title="导出中心" extra={<Tag color="blue">支持 CSV / XLSX / SPSS</Tag>}>
      <Space wrap>
        <Select style={{ width: 280 }} placeholder="选择项目" value={pid} onChange={setPid}
          options={projects.map((p) => ({ value: p.projectId, label: `#${p.projectId} ${p.name}` }))} />
        <Select value={format} onChange={setFormat} options={[{ value: 'csv', label: 'CSV' }, { value: 'xlsx', label: 'XLSX' }, { value: 'sav', label: 'SPSS SAV' }]} />
        <Button type="primary" icon={<DownloadOutlined />} onClick={download} disabled={!pid || selected.length === 0}>导出</Button>
      </Space>
      <Divider />
      <Typography.Text strong>导出列（已选 {selected.length}/{headers.length}）</Typography.Text>
      <div style={{ marginTop: 12 }}>
        <Checkbox.Group value={selected} onChange={(v) => setSelected(v as number[])}>
          <Space direction="vertical">
            {headers.map((h, i) => <Checkbox key={i} value={i}>{i + 1}. {h}</Checkbox>)}
          </Space>
        </Checkbox.Group>
      </div>
    </Card>
    <Card title="导出历史" extra={<Button size="small" danger icon={<DeleteOutlined />} onClick={clearHistory} disabled={!history.length}>清空</Button>}>
      <Table<History> rowKey="key" size="small" pagination={false} dataSource={history} locale={{ emptyText: '暂无导出记录（记录保存在当前浏览器）' }}
        columns={[{ title: '时间', dataIndex: 'at' }, { title: '项目', dataIndex: 'project' }, { title: '格式', dataIndex: 'format', render: (v) => <Tag>{v}</Tag> }, { title: '列数', dataIndex: 'columns' }]} />
    </Card>
  </Space>
}
