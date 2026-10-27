import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button, Card, Form, Input, message } from 'antd'
import { LockOutlined, UserOutlined } from '@ant-design/icons'
import { useAuth } from '../stores/auth'

export default function Login() {
  const [loading, setLoading] = useState(false)
  const nav = useNavigate()
  const login = useAuth((s) => s.login)

  return (
    <div style={{ height: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center',
      background: 'linear-gradient(135deg, #F6E3CC 0%, #EFC49A 55%, #E2A670 100%)' }}>
      <Card style={{ width: 380, boxShadow: '0 8px 30px rgba(90,50,20,.18)', background: '#FFF9F1' }}>
        <div style={{ textAlign: 'center', marginBottom: 24 }}>
          <div style={{ fontSize: 26, fontWeight: 800, color: '#A34713' }}>EvolverMon</div>
          <div style={{ color: '#5C4433', marginTop: 4 }}>开源 ERP · MES · WMS 一体化平台</div>
        </div>
        <Form onFinish={async (v) => {
          setLoading(true)
          try {
            await login(v.username, v.password)
            message.success('登录成功')
            nav('/dashboard')
          } catch { /* interceptor 已提示 */ } finally { setLoading(false) }
        }}>
          <Form.Item name="username" rules={[{ required: true, message: '请输入用户名' }]}>
            <Input prefix={<UserOutlined />} placeholder="用户名（admin / planner / keeper…）" size="large" />
          </Form.Item>
          <Form.Item name="password" rules={[{ required: true, message: '请输入密码' }]}>
            <Input.Password prefix={<LockOutlined />} placeholder="密码（Admin@123 / Demo@123）" size="large" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block size="large" loading={loading}>登 录</Button>
        </Form>
        <div style={{ marginTop: 16, color: '#999', fontSize: 12, textAlign: 'center' }}>
          超级管理员：admin / Admin@123 · 演示账号：planner、keeper、supervisor、qc、sales / Demo@123
        </div>
      </Card>
    </div>
  )
}
