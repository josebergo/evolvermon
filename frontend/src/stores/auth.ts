import { create } from 'zustand'
import { client, post } from '../api/client'

export interface MenuItem {
  id: number; parentId: number; name: string; path: string
  icon?: string; sort?: number; type: string; permCode?: string
  children?: MenuItem[]
}

interface AuthState {
  accessToken: string | null
  refreshToken: string | null
  user: { id: number; username: string; realName: string; isSuper: boolean } | null
  perms: string[]
  menus: MenuItem[]
  factories: { id: number; name: string; code: string }[]
  factoryId: number | null
  factoryName: string
  login: (username: string, password: string) => Promise<void>
  loadProfile: () => Promise<void>
  switchFactory: (id: number) => Promise<void>
  tryRefresh: () => Promise<boolean>
  logout: () => void
  can: (perm: string) => boolean
}

export const useAuth = create<AuthState>((set, get) => ({
  accessToken: localStorage.getItem('em_at'),
  refreshToken: localStorage.getItem('em_rt'),
  user: null, perms: [], menus: [], factories: [], factoryId: null, factoryName: '',

  login: async (username, password) => {
    const data: any = await post('/auth/login', { username, password })
    localStorage.setItem('em_at', data.accessToken)
    localStorage.setItem('em_rt', data.refreshToken)
    set({ accessToken: data.accessToken, refreshToken: data.refreshToken })
    await get().loadProfile()
  },

  loadProfile: async () => {
    const data: any = await client.get('/auth/profile')
    const factories = data.factories || []
    const cur = factories.find((f: any) => f.id === data.factoryId) || factories[0]
    set({
      user: data.user, perms: data.perms || [], menus: data.menus || [],
      factories, factoryId: cur?.id ?? null, factoryName: cur?.name ?? '',
    })
  },

  switchFactory: async (id) => {
    const data: any = await post('/auth/switch-factory', { factoryId: id })
    localStorage.setItem('em_at', data.accessToken)
    set({ accessToken: data.accessToken })
    await get().loadProfile()
  },

  tryRefresh: async () => {
    const rt = get().refreshToken
    if (!rt) return false
    try {
      const data: any = await client.post('/auth/refresh', { refreshToken: rt })
      localStorage.setItem('em_at', data.accessToken)
      set({ accessToken: data.accessToken })
      return true
    } catch {
      return false
    }
  },

  logout: () => {
    localStorage.removeItem('em_at')
    localStorage.removeItem('em_rt')
    set({ accessToken: null, refreshToken: null, user: null, menus: [], perms: [] })
  },

  can: (perm) => {
    const s = get()
    if (!perm) return true
    if (s.user?.isSuper || s.perms.includes('*')) return true
    return s.perms.includes(perm)
  },
}))
