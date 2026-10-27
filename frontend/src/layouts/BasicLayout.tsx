import { useMemo, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Avatar, Dropdown, Layout, Menu, Segmented, Select, Space, Tag } from 'antd'
import {
  DashboardOutlined, SettingOutlined, DatabaseOutlined, ShopOutlined,
  ToolOutlined, ContainerOutlined, LogoutOutlined, UserOutlined, MenuFoldOutlined, MenuUnfoldOutlined,
} from '@ant-design/icons'
import type { MenuProps } from 'antd'
import { useAuth } from '../stores/auth'
import { useLang, menuLabel } from '../stores/lang'
import type { MenuItem } from '../stores/auth'

const { Header, Sider, Content } = Layout

const iconMap: Record<string, JSX.Element> = {
  DashboardOutlined: <DashboardOutlined />,
  SettingOutlined: <SettingOutlined />,
  DatabaseOutlined: <DatabaseOutlined />,
  ShopOutlined: <ShopOutlined />,
  ToolOutlined: <ToolOutlined />,
  ContainerOutlined: <ContainerOutlined />,
}

function toAntMenu(items: MenuItem[]): MenuProps['items'] {
  return items
    .filter((m) => m.type !== 'button')
    .map((m) => ({
      key: m.path,
      icon: m.icon ? iconMap[m.icon] : undefined,
      label: menuLabel(m.name, useLang.getState().lang),
      children: m.children?.length ? toAntMenu(m.children) : undefined,
    }))
}

export default function BasicLayout() {
  const [collapsed, setCollapsed] = useState(false)
  const nav = useNavigate()
  const loc = useLocation()
  const { menus, user, factories, factoryId, factoryName, switchFactory, logout } = useAuth()
  const lang = useLang((s) => s.lang)
  const setLang = useLang((s) => s.setLang)

  const selected = useMemo(() => [loc.pathname], [loc.pathname])
  const openKeys = useMemo(() => {
    const parts = loc.pathname.split('/').filter(Boolean)
    return ['/' + parts[0]]
  }, [loc.pathname])

  const userMenu: MenuProps['items'] = [
    { key: 'name', label: <span>{user?.realName || user?.username}</span>, disabled: true },
    { type: 'divider' },
    { key: 'logout', icon: <LogoutOutlined />, label: '退出登录' },
  ]

  const wmText = encodeURIComponent(
    `<svg xmlns='http://www.w3.org/2000/svg' width='260' height='140'><text x='10' y='80' font-size='14' fill='#5C4433' transform='rotate(-18 130 70)'>${user?.username || 'evolvermon'} · ${new Date().toISOString().slice(0, 10)}</text></svg>`)
  return (
    <Layout style={{ minHeight: '100vh' }}>
      {/* 操作水印（M5）：用户名+日期低透明度平铺，不拦截交互 */}
      <div aria-hidden style={{
        position: 'fixed', inset: 0, zIndex: 999, pointerEvents: 'none', opacity: 0.12,
        backgroundImage: `url("data:image/svg+xml;utf8,${wmText}")`,
        backgroundRepeat: 'repeat',
      }} />
      <Sider collapsible collapsed={collapsed} trigger={null} width={220} theme="light"
        style={{ borderRight: '1px solid #D8BFA6', background: '#FFF6EA' }}>
        <div style={{ height: 56, display: 'flex', alignItems: 'center', justifyContent: 'center', fontWeight: 700, fontSize: 17, color: '#A34713' }}>
          {collapsed ? 'EM' : 'EvolverMon'}
        </div>
        <Menu mode="inline" selectedKeys={selected} defaultOpenKeys={openKeys}
          items={toAntMenu(menus)} onClick={({ key }) => nav(key)} />
      </Sider>
      <Layout>
        <Header style={{ background: '#FFF9F1', padding: '0 16px', display: 'flex', alignItems: 'center',
          justifyContent: 'space-between', borderBottom: '1px solid #D8BFA6', height: 56 }}>
          <div onClick={() => setCollapsed(!collapsed)} style={{ cursor: 'pointer', fontSize: 16 }}>
            {collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
          </div>
          <Space size={16}>
            <Segmented size="small" value={lang} onChange={(v) => setLang(v as 'zh' | 'en')}
              options={[{ value: 'zh', label: '中' }, { value: 'en', label: 'EN' }]} />
            <Tag color="orange">{factoryName || '未选择工厂'}</Tag>
            {factories.length > 1 && (
              <Select size="small" style={{ width: 140 }} value={factoryId ?? undefined}
                onChange={(v) => switchFactory(v)}
                options={factories.map((f) => ({ value: f.id, label: f.name }))} />
            )}
            <Dropdown menu={{ items: userMenu, onClick: ({ key }) => key === 'logout' && (logout(), nav('/login')) }}>
              <Space style={{ cursor: 'pointer' }}>
                <Avatar size="small" icon={<UserOutlined />} />
                <span>{user?.username}</span>
              </Space>
            </Dropdown>
          </Space>
        </Header>
        <Content style={{ margin: 12, minHeight: 360 }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  )
}
