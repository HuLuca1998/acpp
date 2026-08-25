import { useTranslation } from "react-i18next"
import { Navigate, Outlet, useLocation } from "react-router"

import { AppSidebar } from "@/components/shell/app-sidebar"
import { IdentityGate } from "@/components/shell/identity-gate"
import { NotifyMenu } from "@/components/shell/notify-menu"
import { TitleBar } from "@/components/shell/window/title-bar"
import { WindowControls } from "@/components/shell/window/window-controls"
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"
import { useIsOwner } from "@/hooks/identity-context"
import { useNotifications } from "@/hooks/use-notifications"
import { useSidebarFrame } from "@/hooks/use-sidebar-frame"

/**
 * 只有 owner 能进的页面（adr-007）。租户手敲 URL 也进不去——后端对应的
 * 端点已经是 403，这里把人送回会话页，免得他对着一屏加载失败发呆。
 */
const OWNER_ONLY_PREFIXES = [
  "/skills",
  "/settings",
  "/connections",
  "/tools",
  "/logs",
]

/** 路径前缀 → 标题的翻译 key。最长前缀优先。 */
const TITLE_KEYS = [
  ["/skills", "nav.skills"],
  ["/sessions", "nav.sessions"],
  ["/tools", "nav.tools"],
  ["/logs", "nav.logs"],
  ["/settings", "nav.settings"],
  ["/connections", "tenants.title"],
  ["/help", "nav.help"],
  ["/search", "nav.search"],
] as const

export function DashboardLayout() {
  const { t } = useTranslation()
  const { pathname } = useLocation()

  const matched = TITLE_KEYS.find(([prefix]) => pathname.startsWith(prefix))
  const title =
    pathname === "/"
      ? t("nav.overview")
      : matched
        ? t(matched[1])
        : t("common.appName")

  return (
    <IdentityGate>
      <OwnerOnlyRedirect>
        <Shell title={title} />
      </OwnerOnlyRedirect>
    </IdentityGate>
  )
}

/** 租户落在 owner 专属页面（含概览首页）时送回会话页。 */
function OwnerOnlyRedirect({ children }: { children: React.ReactNode }) {
  const { pathname } = useLocation()
  const isOwner = useIsOwner()

  const ownerOnly =
    pathname === "/" || OWNER_ONLY_PREFIXES.some((p) => pathname.startsWith(p))
  if (!isOwner && ownerOnly) {
    return <Navigate to="/sessions" replace />
  }
  return children
}

function Shell({ title }: { title: string }) {
  // 通知挂在 shell 上而不是某个页面：agent 停下来等决策时，用户很可能正停
  // 在别的会话或列表页，哪一页都得知道。（版本更新的提示长在侧栏底部的
  // 状态条里，见 components/shell/backend-status.tsx。）
  useNotifications()
  const frame = useSidebarFrame()

  return (
    <SidebarProvider
      // 锁定整个 shell 到视口高度，滚动交给内容区自己处理，
      // 这样聊天页的输入框才能始终固定在底部。
      className="h-svh"
      style={{ "--sidebar-width": `${frame.width}px` } as React.CSSProperties}
    >
      <AppSidebar variant="inset" frame={frame} />
      {/* 顶部那 8px 收掉：内容区的顶栏要和侧栏的让位条贴着窗口上沿连成一线，
          圆角因此只留下面两角（规范 §5.6）。 */}
      <SidebarInset className="overflow-hidden md:peer-data-[variant=inset]:mt-0! md:peer-data-[variant=inset]:rounded-t-none">
        <TitleBar title={title}>
          <NotifyMenu />
        </TitleBar>
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="@container/main flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto">
            <Outlet />
          </div>
        </div>
      </SidebarInset>
      {/* 全局唯一的一组窗口控件，fixed 钉在窗口左上角——展开、折叠、浮出
          三态下它都不动（规范 §5.6）。放在 provider 内是因为要读侧栏状态。 */}
      <WindowControls frame={frame} />
    </SidebarProvider>
  )
}
