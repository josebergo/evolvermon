import { lazy, Suspense, useEffect } from 'react'
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { Spin } from 'antd'
import BasicLayout from './layouts/BasicLayout'
import { useAuth } from './stores/auth'
import Login from './pages/Login'

const Dashboard = lazy(() => import('./pages/Dashboard'))
const Factories = lazy(() => import('./pages/system/Factories'))
const Warehouses = lazy(() => import('./pages/system/Warehouses'))
const Users = lazy(() => import('./pages/system/Users'))
const Roles = lazy(() => import('./pages/system/Roles'))
const AuditLogs = lazy(() => import('./pages/system/AuditLogs'))
const Params = lazy(() => import('./pages/system/Params'))
const Materials = lazy(() => import('./pages/master/Materials'))
const Boms = lazy(() => import('./pages/master/Boms'))
const Routings = lazy(() => import('./pages/master/Routings'))
const WorkCenters = lazy(() => import('./pages/master/WorkCenters'))
const Customers = lazy(() => import('./pages/master/Customers'))
const Suppliers = lazy(() => import('./pages/master/Suppliers'))
const SalesOrders = lazy(() => import('./pages/erp/SalesOrders'))
const PurchaseRequests = lazy(() => import('./pages/erp/PurchaseRequests'))
const PurchaseOrders = lazy(() => import('./pages/erp/PurchaseOrders'))
const Mrp = lazy(() => import('./pages/erp/Mrp'))
const Atp = lazy(() => import('./pages/erp/Atp'))
const Finance = lazy(() => import('./pages/erp/Finance'))
const WorkOrders = lazy(() => import('./pages/mes/WorkOrders'))
const Kanban = lazy(() => import('./pages/mes/Kanban'))
const Scheduling = lazy(() => import('./pages/mes/Scheduling'))
const BigBoard = lazy(() => import('./pages/mes/Board'))
const Quality = lazy(() => import('./pages/mes/Quality'))
const DefectCodes = lazy(() => import('./pages/mes/DefectCodes'))
const Stocks = lazy(() => import('./pages/wms/Stocks'))
const StockOrders = lazy(() => import('./pages/wms/StockOrders'))
const Movements = lazy(() => import('./pages/wms/Movements'))
const Stocktakings = lazy(() => import('./pages/wms/Stocktakings'))
const Reservations = lazy(() => import('./pages/wms/Reservations'))
const Transfers = lazy(() => import('./pages/wms/Transfers'))
const Group = lazy(() => import('./pages/system/Group'))
const Webhooks = lazy(() => import('./pages/system/Webhooks'))
const Telemetry = lazy(() => import('./pages/mes/Telemetry'))
const PdaReport = lazy(() => import('./pages/pda/Report'))
const PdaStock = lazy(() => import('./pages/pda/Stock'))

function Loading() {
  return <div style={{ padding: 100, textAlign: 'center' }}><Spin size="large" /></div>
}

function RequireAuth({ children }: { children: JSX.Element }) {
  const token = useAuth((s) => s.accessToken)
  const location = useLocation()
  if (!token) return <Navigate to="/login" state={{ from: location }} replace />
  return children
}

export default function App() {
  const loadProfile = useAuth((s) => s.loadProfile)
  const token = useAuth((s) => s.accessToken)
  useEffect(() => {
    if (token) loadProfile().catch(() => {})
  }, [token])

  return (
    <BrowserRouter>
      <Suspense fallback={<Loading />}>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/pda/report" element={<PdaReport />} />
          <Route path="/pda/stock" element={<PdaStock />} />
          <Route path="/" element={<RequireAuth><BasicLayout /></RequireAuth>}>
            <Route index element={<Navigate to="/dashboard" replace />} />
            <Route path="dashboard" element={<Dashboard />} />
            <Route path="system/factories" element={<Factories />} />
            <Route path="system/warehouses" element={<Warehouses />} />
            <Route path="system/users" element={<Users />} />
            <Route path="system/roles" element={<Roles />} />
            <Route path="system/audit-logs" element={<AuditLogs />} />
            <Route path="system/params" element={<Params />} />
            <Route path="master/materials" element={<Materials />} />
            <Route path="master/boms" element={<Boms />} />
            <Route path="master/routings" element={<Routings />} />
            <Route path="master/work-centers" element={<WorkCenters />} />
            <Route path="master/customers" element={<Customers />} />
            <Route path="master/suppliers" element={<Suppliers />} />
            <Route path="erp/sales-orders" element={<SalesOrders />} />
            <Route path="erp/purchase-requests" element={<PurchaseRequests />} />
            <Route path="erp/purchase-orders" element={<PurchaseOrders />} />
            <Route path="erp/mrp" element={<Mrp />} />
            <Route path="erp/atp" element={<Atp />} />
            <Route path="erp/finance" element={<Finance />} />
            <Route path="mes/work-orders" element={<WorkOrders />} />
            <Route path="mes/kanban" element={<Kanban />} />
            <Route path="mes/scheduling" element={<Scheduling />} />
            <Route path="mes/board" element={<BigBoard />} />
            <Route path="mes/telemetry" element={<Telemetry />} />
            <Route path="mes/quality" element={<Quality />} />
            <Route path="mes/defect-codes" element={<DefectCodes />} />
            <Route path="wms/stocks" element={<Stocks />} />
            <Route path="wms/stock-orders" element={<StockOrders />} />
            <Route path="wms/movements" element={<Movements />} />
            <Route path="wms/stocktakings" element={<Stocktakings />} />
            <Route path="wms/reservations" element={<Reservations />} />
            <Route path="wms/transfers" element={<Transfers />} />
            <Route path="system/group" element={<Group />} />
            <Route path="system/webhooks" element={<Webhooks />} />
          </Route>
          <Route path="*" element={<Navigate to="/dashboard" replace />} />
        </Routes>
      </Suspense>
    </BrowserRouter>
  )
}
