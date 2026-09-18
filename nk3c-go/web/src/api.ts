// ResultInfo 信封客户端【教训 S13】：后端恒 HTTP 200，必须判响应体 code 而非 HTTP status
import axios from 'axios'

export interface RInfo<T = unknown> {
  success: boolean
  code: string
  message: string
  data: T
}

export interface SessionUser {
  sessionId: string
  userId: number
  userName: string
  agentNo: string
  roles: string[]
}

const KEY = 'nk3c_session'

export function rawSession(): SessionUser | null {
  try {
    const v = localStorage.getItem(KEY)
    return v ? (JSON.parse(v) as SessionUser) : null
  } catch {
    return null
  }
}
export function saveSession(u: SessionUser) {
  localStorage.setItem(KEY, JSON.stringify(u))
}
export function clearSession() {
  localStorage.removeItem(KEY)
}
export function hasRole(...rs: string[]): boolean {
  const s = rawSession()
  return !!s && rs.some((r) => s.roles.includes(r))
}

const client = axios.create({ baseURL: '', timeout: 15000 })

client.interceptors.request.use((cfg) => {
  const s = rawSession()
  if (s) cfg.headers.Authorization = `Bearer ${s.sessionId}`
  return cfg
})

client.interceptors.response.use((resp): never => {
  const body = resp.data as RInfo
  if (body && typeof body.code === 'string') {
    if (body.code === '4010') {
      clearSession()
      window.dispatchEvent(new Event('nk3c:unauthorized'))
    }
  }
  // 信封直通（绕开 AxiosResponse 形状约束，调用侧一律拿到 ResultInfo）
  return body as never
})

type Call = <T>(url: string, body?: unknown) => Promise<RInfo<T>>

export const api: { get: Call; post: Call; put: Call } = {
  get: (url) => client.get<never, RInfo>(url) as never,
  post: (url, body) => client.post<never, RInfo>(url, body ?? {}) as never,
  put: (url, body) => client.put<never, RInfo>(url, body ?? {}) as never,
}

// 通用解包：成功取 data，失败抛消息（配合 try/catch + antd message）
export async function unwrap<T>(p: Promise<RInfo<T>>): Promise<T> {
  const r = await p
  if (!r.success) throw new Error(r.message)
  return r.data
}
