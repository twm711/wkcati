import { useCallback, useEffect, useState } from 'react'
import { App, Button, Card, Popconfirm, Space, Table, Tag, Typography } from 'antd'
import { ReloadOutlined, RollbackOutlined } from '@ant-design/icons'
import { api, hasRole } from '../api'

interface DeadTask { taskId:number; projectId:number; sampleId:number; callId:number; retryCount:number; maxRetries:number; completedAt:string; custName:string }

export default function DeadTasks() {
  const { message } = App.useApp()
  const [rows, setRows] = useState<DeadTask[]>([])
  const [loading, setLoading] = useState(false)
  const canManage = hasRole('groupAdmin', 'orgAdmin', 'domainAdmin')
  const load = useCallback(async () => {
    setLoading(true)
    try {
      const r = await api.get<DeadTask[]>('/api/agent/dead-tasks')
      if (r.success) setRows(r.data || [])
      else message.error(r.message)
    } finally { setLoading(false) }
  }, [message])
  useEffect(() => { load() }, [load])
  const retry = async (sampleId:number) => {
    const r = await api.post(`/api/agent/dead-tasks/${sampleId}/retry`)
    if (!r.success) { message.error(r.message); return }
    message.success(r.message); load()
  }
  return <Card title="死信任务" extra={<Button icon={<ReloadOutlined />} onClick={load}>刷新</Button>}>
    <Typography.Paragraph type="secondary">达到最大重试次数的样本会进入死信。恢复后重新进入样本池，并保留原任务尝试历史。</Typography.Paragraph>
    <Table rowKey="taskId" loading={loading} dataSource={rows} pagination={{ pageSize: 20 }} columns={[
      { title:'任务', dataIndex:'taskId' }, { title:'样本', dataIndex:'sampleId' }, { title:'客户', dataIndex:'custName' },
      { title:'项目', dataIndex:'projectId' }, { title:'重试次数', render:(_,r)=> <Tag color="error">{r.retryCount} / {r.maxRetries}</Tag> },
      { title:'进入时间', dataIndex:'completedAt' }, { title:'操作', hidden:!canManage, render:(_,r)=><Popconfirm title="确认重新入池？" onConfirm={()=>retry(r.sampleId)}><Button type="link" icon={<RollbackOutlined />}>重新入池</Button></Popconfirm> },
    ]} />
    {!canManage && <Space><Typography.Text type="secondary">仅督导及以上角色可恢复死信任务。</Typography.Text></Space>}
  </Card>
}
