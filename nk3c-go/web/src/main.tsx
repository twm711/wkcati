import React from 'react'
import ReactDOM from 'react-dom/client'
import { ConfigProvider, App as AntApp } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { HashRouter, Routes, Route, Navigate } from 'react-router-dom'
import 'dayjs/locale/zh-cn'
import Layout from './layout'
import Login from './pages/Login'
import Agent from './pages/Agent'
import Monitor from './pages/Monitor'
import Projects from './pages/Projects'
import Export from './pages/Export'
import Workorders from './pages/Workorders'
import Ivr from './pages/Ivr'
import DeadTasks from './pages/DeadTasks'
import Lines from './pages/Lines'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ConfigProvider locale={zhCN} theme={{ token: { colorPrimary: '#1677ff' } }}>
      <AntApp>
        <HashRouter>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route element={<Layout />}>
              <Route path="/agent" element={<Agent />} />
              <Route path="/monitor" element={<Monitor />} />
              <Route path="/projects" element={<Projects />} />
              <Route path="/exports" element={<Export />} />
              <Route path="/workorders" element={<Workorders />} />
              <Route path="/ivr" element={<Ivr />} />
              <Route path="/dead-tasks" element={<DeadTasks />} />
              <Route path="/lines" element={<Lines />} />
            </Route>
            <Route path="*" element={<Navigate to="/agent" replace />} />
          </Routes>
        </HashRouter>
      </AntApp>
    </ConfigProvider>
  </React.StrictMode>,
)
