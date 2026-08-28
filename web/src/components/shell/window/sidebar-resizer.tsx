import { useTranslation } from "react-i18next"

/**
 * 侧栏右沿的拖宽把手。平时看不见，悬停与拖动时亮一条线；双击复位。
 *
 * 定位基准是 sidebar-container（它是 fixed，天然是定位上下文），所以贴的是
 * 侧栏视觉边缘而不是内容边缘。折叠态下侧栏是临时浮出的，宽度没有留存的意义，
 * 调用方那时不渲染它。
 */
export function SidebarResizer({
  onPointerDown,
  onDoubleClick,
}: {
  onPointerDown: (event: React.PointerEvent<HTMLElement>) => void
  onDoubleClick: () => void
}) {
  const { t } = useTranslation()

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={t("nav.resizeSidebar")}
      title={t("nav.resizeSidebar")}
      onPointerDown={onPointerDown}
      onDoubleClick={onDoubleClick}
      className="absolute inset-y-0 end-0 z-20 w-2 cursor-col-resize after:absolute after:inset-y-0 after:start-1/2 after:w-px after:-translate-x-1/2 after:bg-sidebar-ring after:opacity-0 after:transition-opacity after:duration-150 hover:after:opacity-60 in-[body[data-resizing]]:after:opacity-60"
    />
  )
}
