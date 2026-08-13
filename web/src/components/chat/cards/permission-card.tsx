import { useState } from "react"
import { useTranslation } from "react-i18next"

import type { PendingPermission } from "@/types/acp"
import {
  ToolCallBlock,
  type ToolCallPayload,
} from "@/components/chat/tool-call"
import { Button } from "@/components/ui/button"
import { ShieldIcon } from "lucide-react"

/**
 * 权限裁决卡片：agent 阻塞等用户点选。工具详情复用 ToolCallBlock
 * （claude 带 title/diff，codex 只有 kind——按空值收敛），
 * 选项按钮用 agent 给的原文（Allow / Always Allow… / Reject）。
 */
export function PermissionCard({
  permission,
  onResolve,
}: {
  permission: PendingPermission
  /** optionId 为空表示取消；choiceName 用于本轮内的裁决记录。 */
  onResolve: (optionId: string, choiceName: string) => void
}) {
  const { t } = useTranslation()
  const [submitted, setSubmitted] = useState(false)

  const hasDetail = Boolean(
    permission.title || permission.rawInput || permission.content
  )

  return (
    <div className="rounded-xl border border-border bg-card p-4 shadow-sm">
      <div className="mb-3 flex items-center gap-2 text-sm font-medium">
        <ShieldIcon className="size-4 text-warning" />
        {t("chat.permission.title")}
      </div>

      {hasDetail ? (
        <div className="mb-3">
          <ToolCallBlock
            title={permission.title || permission.toolKind || ""}
            payload={{
              kind: permission.toolKind,
              rawInput: permission.rawInput as ToolCallPayload["rawInput"],
              content: permission.content as ToolCallPayload["content"],
            }}
          />
        </div>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        {permission.options.map((option) => {
          const rejects = option.kind.startsWith("reject")
          return (
            <Button
              key={option.optionId}
              size="sm"
              variant={rejects ? "outline" : "default"}
              className={rejects ? "text-destructive" : undefined}
              disabled={submitted}
              onClick={() => {
                setSubmitted(true)
                onResolve(option.optionId, option.name)
              }}
            >
              {option.name}
            </Button>
          )
        })}
      </div>
    </div>
  )
}
