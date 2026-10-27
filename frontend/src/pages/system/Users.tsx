import { Form, Input, Select } from 'antd'
import CrudTable from '../../components/CrudTable'

export default function Users() {
  return (
    <CrudTable
      title="用户管理（角色与工厂在「角色权限」页分配）"
      api="/users"
      searchPlaceholder="用户名/姓名"
      columns={[
        { title: '用户名', dataIndex: 'username', width: 120 },
        { title: '姓名', dataIndex: 'realName', width: 120 },
        { title: '手机', dataIndex: 'mobile', width: 130 },
        { title: '邮箱', dataIndex: 'email' },
        { title: '超管', dataIndex: 'isSuper', width: 70, render: (v) => (v ? '是' : '') },
        { title: '状态', dataIndex: 'status', width: 80, render: (v) => (v === 1 ? '启用' : '停用') },
        { title: '最近登录', dataIndex: 'lastLoginAt', width: 160, render: (v) => v?.slice(0, 19).replace('T', ' ') },
      ]}
      formItems={() => (
        <>
          <Form.Item name="username" label="用户名" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="realName" label="姓名" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="mobile" label="手机"><Input /></Form.Item>
          <Form.Item name="email" label="邮箱"><Input /></Form.Item>
          <Form.Item name="status" label="状态" initialValue={1}><Select options={[{ value: 1, label: '启用' }, { value: 0, label: '停用' }]} /></Form.Item>
        </>
      )}
    />
  )
}
