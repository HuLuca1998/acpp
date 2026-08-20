import { useContext } from "react"
import { useTranslation } from "react-i18next"
import { FilePenLineIcon } from "lucide-react"

import { relativePath } from "@/lib/format"
import { Hint } from "@/components/hint"
import { WorkspaceContext } from "@/components/workspace/workspace-context"
import type { ToolLocation } from "@/types/acp"

/**
 * 「正在触碰」一行小字：busy 期间显示 agent 最近动的文件（ACP tool_call
 * 的 locations，follow-along 语义），点击在文件查看器里打开并定位到行。
 * 没有工作区上下文（无 dock 的流）时退化成纯展示，不承诺点不动的交互。
 */
export function TouchedFile({ loc, cwd }: { loc: ToolLocation; cwd?: string }) {
  const { t } = useTranslation()
  const ws = useContext(WorkspaceContext)
  const label = relativePath(loc.path, cwd) + (loc.line ? `:${loc.line}` : "")
  const body = (
    <>
      <FilePenLineIcon className="size-3 shrink-0" />
      <span className="truncate font-mono">{label}</span>
    </>
  )

  if (!ws) {
    return (
      <span
        title={loc.path}
        className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground/80"
      >
        {body}
      </span>
    )
  }
  return (
    <Hint label={t("chat.touchedHint")} desc={loc.path} align="start">
      <button
        type="button"
        onClick={() => ws.openPreview(loc.path, loc.line)}
        className="flex min-w-0 items-center gap-1.5 rounded-md text-xs text-muted-foreground/80 transition-colors duration-150 outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        {body}
      </button>
    </Hint>
  )
}
