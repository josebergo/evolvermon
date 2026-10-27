import React from 'react'
import ReactDOM from 'react-dom/client'
import { ConfigProvider, App as AntApp } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import enUS from 'antd/locale/en_US'
import dayjs from 'dayjs'
import 'dayjs/locale/zh-cn'
import App from './App'
import './index.css'
import { useLang } from './stores/lang'

dayjs.locale('zh-cn')

// 暖色系高对比主题：赤陶橙主色 + 暖白底 + 深褐近黑正文
const warmTheme = {
  token: {
    colorPrimary: '#C2551B',        // 赤陶橙（深，保证白字对比度 ≥4.5:1）
    colorInfo: '#C2551B',
    colorSuccess: '#3E7D1F',        // 暖绿（深）
    colorWarning: '#B36B00',        // 琥珀（深）
    colorError: '#B3251B',          // 暖红（深）
    colorTextBase: '#26160B',       // 深褐近黑正文（对暖白底对比度 ~15:1）
    colorBgBase: '#FBF3EA',         // 暖白底
    colorBgContainer: '#FFF9F1',
    colorBgLayout: '#F4E8DA',       // 布局底：暖沙色
    colorBorder: '#D8BFA6',
    colorBorderSecondary: '#E8D5C0',
    colorLink: '#A34713',
    borderRadius: 6,
    fontSize: 14,
  },
}

function Root() {
  const lang = useLang((s) => s.lang)
  return (
    <ConfigProvider locale={lang === 'en' ? enUS : zhCN} theme={warmTheme}>
      <AntApp>
        <App />
      </AntApp>
    </ConfigProvider>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <Root />
  </React.StrictMode>,
)
