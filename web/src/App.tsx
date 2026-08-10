import { Route, Routes } from "react-router"

import { Agents } from "@/routes/agents"
import { DashboardLayout } from "@/routes/dashboard-layout"
import { NotFound } from "@/routes/not-found"
import { Overview } from "@/routes/overview"
import { Placeholder } from "@/routes/placeholder"
import { Sessions } from "@/routes/sessions"

export function App() {
  return (
    <Routes>
      <Route element={<DashboardLayout />}>
        <Route index element={<Overview />} />
        <Route path="agents" element={<Agents />} />
        <Route
          path="agents/new"
          element={
            <Placeholder
              title="Add agent"
              description="配置 agent 的启动命令、参数与工作目录。"
            />
          }
        />
        <Route
          path="agents/:id"
          element={
            <Placeholder
              title="Agent detail"
              description="展示 initialize 返回的能力集与最近会话。"
            />
          }
        />
        <Route path="sessions" element={<Sessions />} />
        <Route
          path="sessions/new"
          element={
            <Placeholder
              title="New session"
              description="选择 agent 并发起 session/new。"
            />
          }
        />
        <Route
          path="sessions/:id"
          element={
            <Placeholder
              title="Session detail"
              description="消息流、工具调用与权限请求。"
            />
          }
        />
        <Route
          path="tools"
          element={
            <Placeholder
              title="Tools"
              description="agent 暴露的工具与调用统计。"
            />
          }
        />
        <Route
          path="logs"
          element={
            <Placeholder
              title="Logs"
              description="JSON-RPC 原始收发日志。"
            />
          }
        />
        <Route
          path="settings"
          element={
            <Placeholder
              title="Settings"
              description="默认工作目录、权限策略与数据库位置。"
            />
          }
        />
        <Route
          path="connections"
          element={
            <Placeholder
              title="Connections"
              description="当前活跃的 stdio 连接与握手状态。"
            />
          }
        />
        <Route
          path="help"
          element={
            <Placeholder
              title="Get Help"
              description="ACP 协议文档与常见问题。"
            />
          }
        />
        <Route
          path="search"
          element={
            <Placeholder
              title="Search"
              description="跨会话与消息的全文检索。"
            />
          }
        />
      </Route>
      <Route path="*" element={<NotFound />} />
    </Routes>
  )
}

export default App
