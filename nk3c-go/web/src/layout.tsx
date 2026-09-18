import { useEffect, useState } from 'react'
import { Layout as AntLayout, Menu, Button, Tag, Space, Typography } from 'antd'
import {
  CustomerServiceOutlined,
  DashboardOutlined,
  AppstoreOutlined,
  FileDoneOutlined,
  AudioOutlined,
  LogoutOutlined,
} from '@ant-design/icons'
import { Outlet, useLocation, useNavigate, Navigate } from 'react-router-dom'
import { clearSession, rawSession, type SessionUser } from './api'

const ROLE_LABEL: Record<string, string> = {
  domainAdmin: '系统管理员',
  orgAdmin: '机构管理',
  groupAdmin: '督导',
  phoneAdmin: '坐席',
}

export default function Layout() {
  const [user, setUser] = useState<SessionUser | null>(rawSession())
  const nav = useNavigate()
  const loc = useLocation()

  useEffect(() => {
    const onUnauthorized = () => {
      setUser(null)
      nav('/login')
    }
    window.addEventListener('nk3c:unauthorized', onUnauthorized)
    return () => window.removeEventListener('nk3c:unauthorized', onUnauthorized)
  }, [nav])

  if (!user) return <Navigate to="/login" replace />

  const logout = async () => {
    clearSession()
    nav('/login')
  }

  return (
    <AntLayout style={{ minHeight: '100vh' }}>
      <AntLayout.Sider theme="dark" width={200}>
        <div style={{ color: '#fff', padding: '18px 16px 10px', fontWeight: 700, fontSize: 16 }}>
          NK-3C 呼出中心
          <div style={{ fontWeight: 400, fontSize: 11, opacity: 0.7, marginTop: 2 }}>
            ITACATI · Go 版控制台
          </div>
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[loc.pathname]}
          onClick={(e) => nav(e.key)}
          items={[
            { key: '/agent', icon: <CustomerServiceOutlined />, label: '坐席工作台' },
            { key: '/monitor', icon: <DashboardOutlined />, label: '监控墙 / 质检' },
            { key: '/projects', icon: <AppstoreOutlined />, label: '项目管理' },
            { key: '/workorders', icon: <FileDoneOutlined />, label: '工单中心' },
            { key: '/ivr', icon: <AudioOutlined />, label: 'IVR 互动' },
          ]}
        />
      </AntLayout.Sider>
      <AntLayout>
        <AntLayout.Header
          style={{ background: '#fff', display: 'flex', justifyContent: 'flex-end', alignItems: 'center', paddingInline: 24 }}
        >
          <Space>
            <Typography.Text strong>{user.userName}</Typography.Text>
            {user.agentNo && <Tag color="blue">坐席号 {user.agentNo}</Tag>}
            {user.roles
              .filter((r) => ROLE_LABEL[r])
              .map((r) => (
                <Tag key={r} color="geekblue">
                  {ROLE_LABEL[r]}
                </Tag>
              ))}
            <Button size="small" icon={<LogoutOutlined />} onClick={logout}>
              签出
            </Button>
          </Space>
        </AntLayout.Header>
        <AntLayout.Content style={{ padding: 20 }}>
          <Outlet context={{ user, setUser } satisfies { user: SessionUser | null; setUser: (u: SessionUser) => void }} />
        </AntLayout.Content>
      </AntLayout>
    </AntLayout>
  )
}
