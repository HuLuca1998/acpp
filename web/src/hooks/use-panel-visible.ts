import { useCallback, useEffect, useRef, useState } from "react"
import type { IDockviewPanelProps } from "dockview-react"

type PanelApi = IDockviewPanelProps["api"]

/** dockview 面板当前是否可见（所在 tab 组里被选中的那个）。 */
export function usePanelVisible(api: PanelApi): boolean {
  const [visible, setVisible] = useState(api.isVisible)
  useEffect(() => {
    const disposable = api.onDidVisibilityChange((e) => setVisible(e.isVisible))
    return () => disposable.dispose()
  }, [api])
  return visible
}

/**
 * 面板的「打开即加载 + 工作区刷新跟进」，但只在可见时真的干活：
 * 藏在 tab 后面的面板收到刷新广播只记一笔账，切回来那一刻补一次。
 * 用户看不到的面板不该在每轮结束时默默拉数据。
 *
 * load 引用变化视为数据源变了（切会话 / 换 scope），同样标脏等可见时重拉。
 */
export function useVisibleLoad(
  api: PanelApi,
  onRefresh: (listener: () => void) => () => void,
  load: () => void
): void {
  const visible = usePanelVisible(api)
  const visibleRef = useRef(visible)
  const dirty = useRef(true)

  useEffect(() => {
    visibleRef.current = visible
  }, [visible])

  useEffect(() => {
    dirty.current = true
  }, [load])

  useEffect(() => {
    if (visible && dirty.current) {
      dirty.current = false
      load()
    }
  }, [visible, load])

  const markOrLoad = useCallback(() => {
    if (visibleRef.current) {
      load()
    } else {
      dirty.current = true
    }
  }, [load])
  useEffect(() => onRefresh(markOrLoad), [onRefresh, markOrLoad])
}
