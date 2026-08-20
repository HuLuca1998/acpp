import { useTranslation } from "react-i18next"
import { Route, Routes } from "react-router"

import { DashboardLayout } from "@/routes/dashboard-layout"
import { Databases } from "@/routes/databases"
import { NotFound } from "@/routes/not-found"
import { Overview } from "@/routes/overview"
import { Placeholder } from "@/routes/placeholder"
import { SessionChat } from "@/routes/session-chat"
import { Sessions } from "@/routes/sessions"
import { Tenants } from "@/routes/tenants"
import { Settings } from "@/routes/settings"
import { SkillDetail } from "@/routes/skill-detail"
import { Skills } from "@/routes/skills"

/** 尚未实现、但已在导航里占位的页面。 */
const PLACEHOLDERS = [
  { path: "tools", titleKey: "nav.tools", descKey: "placeholderPage.tools" },
  { path: "logs", titleKey: "nav.logs", descKey: "placeholderPage.logs" },
  { path: "help", titleKey: "nav.help", descKey: "placeholderPage.help" },
  { path: "search", titleKey: "nav.search", descKey: "placeholderPage.search" },
] as const

export function App() {
  const { t } = useTranslation()

  return (
    <Routes>
      <Route element={<DashboardLayout />}>
        <Route index element={<Overview />} />
        <Route path="skills" element={<Skills />} />
        {/* draft-first：新建直接进空白详情页，首条保存才创建（无 name 参数）。 */}
        <Route path="skills/new" element={<SkillDetail />} />
        <Route path="skills/:name" element={<SkillDetail />} />
        <Route path="sessions" element={<Sessions />} />
        {/* 新会话与老会话共用同一个页面：草稿态只是多了跨 ACP 模型选择
            与可编辑工作目录，首条消息落地才真正创建会话。 */}
        <Route path="sessions/new" element={<SessionChat />} />
        <Route path="sessions/:id" element={<SessionChat />} />
        {/* 数据库连接（adr-008）：按项目 + 环境管理，会话侧只看得到本项目的。 */}
        <Route path="databases" element={<Databases />} />
        <Route path="settings" element={<Settings />} />
        {/* 「连接」= 局域网访客管理（adr-007）：发链接、看谁在用、随时关停。 */}
        <Route path="connections" element={<Tenants />} />
        {PLACEHOLDERS.map(({ path, titleKey, descKey }) => (
          <Route
            key={path}
            path={path}
            element={
              <Placeholder title={t(titleKey)} description={t(descKey)} />
            }
          />
        ))}
      </Route>
      <Route path="*" element={<NotFound />} />
    </Routes>
  )
}

export default App
