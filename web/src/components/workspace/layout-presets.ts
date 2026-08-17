import type { AddPanelOptions, DockviewApi } from "dockview-react"

import { panelKindOf } from "@/components/workspace/workspace-panels"

/** 内置布局预设（adr-002 §2.4）：同一引擎的四种起点，应用后仍可自由拖调。 */
export type LayoutPreset =
  "default" | "ide" | "review" | "git" | "terminalBench"

export const LAYOUT_PRESETS: readonly LayoutPreset[] = [
  "default",
  "ide",
  "review",
  "git",
  "terminalBench",
]

/** 容器完成真实测量（挂载瞬间是 100px 占位）后再定比例，约 2s 兜底放弃。 */
function afterMeasure(api: DockviewApi, apply: () => void) {
  let frames = 0
  const tick = () => {
    if (api.width >= 480) {
      apply()
      return
    }
    if (frames++ < 120) requestAnimationFrame(tick)
  }
  requestAnimationFrame(tick)
}

function setWidth(api: DockviewApi, id: string, ratio: number) {
  api.getPanel(id)?.api.setSize({ width: Math.round(api.width * ratio) })
}

function setHeight(api: DockviewApi, id: string, ratio: number) {
  api.getPanel(id)?.api.setSize({ height: Math.round(api.height * ratio) })
}

/**
 * 应用一套布局预设。现有终端实例面板（pty 还活着）会在新布局里
 * 原样重建（termId 保留，重连恢复现场），没有实例时放一个惰性
 * 待命 tab（首次点到才 spawn）。
 */
export function applyLayoutPreset(api: DockviewApi, preset: LayoutPreset) {
  // 记住活着的终端实例，clear 后回填。
  const terminals = api.panels
    .filter((p) => panelKindOf(p.id) === "terminal")
    .map((p) => ({ id: p.id, params: { ...p.params } }))
  if (terminals.length === 0) {
    terminals.push({ id: "terminal:boot", params: {} })
  }
  const addTerminal = (
    position: AddPanelOptions["position"],
    inactive = true
  ) => {
    terminals.forEach((term, index) => {
      api.addPanel({
        id: term.id,
        component: "terminal",
        renderer: "always",
        inactive: inactive || index > 0,
        params: term.params,
        position:
          index === 0
            ? position
            : { referencePanel: terminals[0].id, direction: "within" },
      })
    })
  }

  api.clear()

  switch (preset) {
    case "ide": {
      // 文件树窄列靠左 ｜ 对话居中 ｜ 右 tab 组，终端横贯底部。
      api.addPanel({ id: "files", component: "files" })
      api.addPanel({
        id: "chat",
        component: "chat",
        minimumWidth: 320,
        position: { referencePanel: "files", direction: "right" },
      })
      api.addPanel({
        id: "changes",
        component: "changes",
        position: { referencePanel: "chat", direction: "right" },
      })
      for (const id of ["preview", "history"] as const) {
        api.addPanel({
          id,
          component: id,
          inactive: true,
          position: { referencePanel: "changes", direction: "within" },
        })
      }
      addTerminal({ direction: "below" })
      afterMeasure(api, () => {
        setWidth(api, "files", 0.18)
        setWidth(api, "chat", 0.46)
        setHeight(api, terminals[0].id, 0.3)
      })
      break
    }
    case "review": {
      // 对话左 36%，变更清单大块细读，文件查看器伴随在下。
      api.addPanel({ id: "chat", component: "chat", minimumWidth: 320 })
      api.addPanel({
        id: "changes",
        component: "changes",
        position: { referencePanel: "chat", direction: "right" },
      })
      api.addPanel({
        id: "preview",
        component: "preview",
        position: { referencePanel: "changes", direction: "below" },
      })
      afterMeasure(api, () => {
        setWidth(api, "chat", 0.36)
        setHeight(api, "preview", 0.55)
      })
      break
    }
    case "git": {
      // 组合窗口（用户要的那张图）：对话在上，git 四件套横贯底部——
      // 左分支 ｜ 中提交链路 ｜ 右上变更 ｜ 右下详情。四个面板独立可关，
      // 这里只是把它们一次摆好。
      api.addPanel({ id: "chat", component: "chat", minimumWidth: 320 })
      api.addPanel({
        id: "branches",
        component: "branches",
        position: { referencePanel: "chat", direction: "below" },
      })
      api.addPanel({
        id: "history",
        component: "history",
        position: { referencePanel: "branches", direction: "right" },
      })
      api.addPanel({
        id: "changes",
        component: "changes",
        position: { referencePanel: "history", direction: "right" },
      })
      api.addPanel({
        id: "detail",
        component: "detail",
        position: { referencePanel: "changes", direction: "below" },
      })
      afterMeasure(api, () => {
        setHeight(api, "branches", 0.42)
        setWidth(api, "branches", 0.18)
        setWidth(api, "history", 0.46)
        setHeight(api, "detail", 0.5)
      })
      break
    }
    case "terminalBench": {
      // 上排对话 ｜ 工具组，下排终端横贯。
      api.addPanel({ id: "chat", component: "chat", minimumWidth: 320 })
      api.addPanel({
        id: "files",
        component: "files",
        position: { referencePanel: "chat", direction: "right" },
      })
      for (const id of ["preview", "changes", "history"] as const) {
        api.addPanel({
          id,
          component: id,
          inactive: true,
          position: { referencePanel: "files", direction: "within" },
        })
      }
      addTerminal({ direction: "below" }, false)
      afterMeasure(api, () => {
        setWidth(api, "chat", 0.5)
        setHeight(api, terminals[0].id, 0.36)
      })
      break
    }
    default: {
      // 纯对话铺满（2026-08-12 定稿变更）：工作区面板全部按需唤起
      //（⋯ 菜单勾选 / 树→预览等联动命令），首屏零工作区开销。
      api.addPanel({ id: "chat", component: "chat", minimumWidth: 320 })
    }
  }
}
