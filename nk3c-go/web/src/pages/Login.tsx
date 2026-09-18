import { useState } from 'react'
import { Card, Form, Input, Button, App, Typography, Space, Tag } from 'antd'
import { useNavigate } from 'react-router-dom'
import { api, saveSession, type SessionUser } from '../api'

const QUICK = [
  { name: 'agent01', desc: '坐席 1020' },
  { name: 'sup01', desc: '督导' },
  { name: 'admin', desc: '系统管理员' },
]

export default function Login() {
  const [loading, setLoading] = useState(false)
  const nav = useNavigate()
  const { message } = App.useApp()
  const [form] = Form.useForm<{ loginName: string; password: string }>()

  const doLogin = async (v: { loginName: string; password: string }) => {
    setLoading(true)
    try {
      const r = await api.post<SessionUser>('/api/auth/login', v)
      if (!r.success) throw new Error(r.message)
      saveSession(r.data)
      message.success(`欢迎，${r.data.userName}`)
      nav('/agent')
    } catch (e) {
      message.error(e instanceof Error ? e.message : '登录失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{ minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', background: 'linear-gradient(135deg,#1677ff22,#f0f2f5)' }}>
      <Card style={{ width: 400 }}>
        <Typography.Title level={4} style={{ textAlign: 'center', marginBottom: 4 }}>
          NK-3C 呼出中心
        </Typography.Title>
        <Typography.Paragraph type="secondary" style={{ textAlign: 'center', fontSize: 12 }}>
          ITACATI · Go + React 技术栈演示
        </Typography.Paragraph>
        <Form form={form} layout="vertical" onFinish={doLogin} initialValues={{ loginName: 'agent01', password: '123456' }}>
          <Form.Item name="loginName" label="登录名" rules={[{ required: true, message: '请输入登录名' }]}>
            <Input placeholder="agent01 / sup01 / admin" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true, message: '请输入密码' }]}>
            <Input.Password />
          </Form.Item>
          <Button type="primary" htmlType="submit" block loading={loading}>
            登录
          </Button>
        </Form>
        <Space wrap style={{ marginTop: 16 }}>
          {QUICK.map((q) => (
            <Tag.CheckableTag key={q.name} checked={false} onChange={() => form.setFieldsValue({ loginName: q.name, password: '123456' })}>
              {q.name}（{q.desc}）
            </Tag.CheckableTag>
          ))}
        </Space>
        <Typography.Paragraph type="secondary" style={{ fontSize: 11, marginTop: 12, marginBottom: 0 }}>
          演示密码统一 123456；数据存于 Go 后端 SQLite（种子可经 /api/sys/reset 重置）。
        </Typography.Paragraph>
      </Card>
    </div>
  )
}
