import { lazy, Suspense, type ComponentType } from "react"
import { useTranslation } from "react-i18next"
import { Route, Routes } from "react-router"

import { DashboardLayout } from "@/routes/dashboard-layout"
import { NotFound } from "@/routes/not-found"
import { Placeholder } from "@/routes/placeholder"
import { Spinner } from "@/components/ui/spinner"

/**
 * 页面按路由懒加载。
 *
 * 不这么做的话所有页面的依赖都进首包，而它们的大头恰恰是各用各的：概览的
 * 图表库、工作区的停靠框架与终端模拟器、对话的 markdown 解析链——打开一条
 * 会话要先下载并解析一遍从没打开过的图表库。拆开之后每个页面只付自己的账，
 * 而且应用更新时没变的那些库还能命中浏览器缓存。
 *
 * 外壳（DashboardLayout）与两个极小的页面（未找到、占位）留在首包：它们
 * 每次都要用，单独发一次请求不划算。
 */
const page = <T extends string>(
  load: () => Promise<Record<T, ComponentType>>,
  name: T
) => lazy(() => load().then((m) => ({ default: m[name] })))

const Overview = page(() => import("@/routes/overview"), "Overview")
const Skills = page(() => import("@/routes/skills"), "Skills")
const SkillDetail = page(() => import("@/routes/skill-detail"), "SkillDetail")
const Sessions = page(() => import("@/routes/sessions"), "Sessions")
const SessionChat = page(() => import("@/routes/session-chat"), "SessionChat")
const Databases = page(() => import("@/routes/databases"), "Databases")
const Servers = page(() => import("@/routes/servers"), "Servers")
const Tools = page(() => import("@/routes/tools"), "Tools")
const Discord = page(() => import("@/routes/discord"), "Discord")
const Jobs = page(() => import("@/routes/jobs"), "Jobs")
const Settings = page(() => import("@/routes/settings"), "Settings")
const Tenants = page(() => import("@/routes/tenants"), "Tenants")

/** 尚未实现、但已在导航里占位的页面。 */
const PLACEHOLDERS = [
  { path: "logs", titleKey: "nav.logs", descKey: "placeholderPage.logs" },
  { path: "help", titleKey: "nav.help", descKey: "placeholderPage.help" },
  { path: "search", titleKey: "nav.search", descKey: "placeholderPage.search" },
] as const

/**
 * 页面分片在途时的占位。
 *
 * 刻意先隐身再淡入（`starting:` + 200ms 延迟）：本机加载一个分片是几毫秒的
 * 事，转圈还没画出来页面就到了——闪一下的 spinner 比空白更像卡顿。真的慢到
 * 需要交代时它才现身。
 */
function RouteFallback() {
  return (
    <div
      className="flex h-full min-h-64 items-center justify-center opacity-100 transition-opacity delay-200 duration-200 starting:opacity-0"
      role="status"
    >
      <Spinner className="size-5 text-muted-foreground" />
    </div>
  )
}

export function App() {
  const { t } = useTranslation()

  return (
    <Suspense fallback={<RouteFallback />}>
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
          <Route path="servers" element={<Servers />} />
          {/* 工具台：我方 MCP server 暴露给 agent 的工具，人工查看与试运行。 */}
          <Route path="tools" element={<Tools />} />
          {/* discord 频道工作区绑定管理（adr-016），与会话体系完全独立。 */}
          <Route path="discord" element={<Discord />} />
          {/* 定时任务（adr-020）挂在 discord 绑定上，独立入口只为少翻一层。 */}
          <Route path="jobs" element={<Jobs />} />
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
    </Suspense>
  )
}

export default App
