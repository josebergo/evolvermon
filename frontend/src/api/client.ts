import axios from 'axios'
import { message } from 'antd'
import { useAuth } from '../stores/auth'

export const client = axios.create({ baseURL: '/api/v1', timeout: 30000 })

client.interceptors.request.use((cfg) => {
  const token = useAuth.getState().accessToken
  if (token) cfg.headers.Authorization = `Bearer ${token}`
  return cfg
})

client.interceptors.response.use(
  (resp) => {
    const body = resp.data
    if (body && body.code !== undefined && body.code !== 0) {
      message.error(body.message || '请求失败')
      return Promise.reject(new Error(body.message))
    }
    return body?.data !== undefined ? body.data : body
  },
  async (err) => {
    const status = err.response?.status
    const msg = err.response?.data?.message
    if (status === 401) {
      const refreshed = await useAuth.getState().tryRefresh()
      if (refreshed) {
        // 重放原请求
        return client(err.config)
      }
      useAuth.getState().logout()
      message.error('登录已过期，请重新登录')
    } else {
      message.error(msg || err.message || '网络异常')
    }
    return Promise.reject(err)
  },
)

// 统一泛型 GET（带分页参数）
export async function get<T = any>(url: string, params?: any): Promise<T> {
  return client.get(url, { params }) as Promise<T>
}
export async function post<T = any>(url: string, data?: any): Promise<T> {
  return client.post(url, data) as Promise<T>
}
export async function put<T = any>(url: string, data?: any): Promise<T> {
  return client.put(url, data) as Promise<T>
}
export async function del<T = any>(url: string): Promise<T> {
  return client.delete(url) as Promise<T>
}
