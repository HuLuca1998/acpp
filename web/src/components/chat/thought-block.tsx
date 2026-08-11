import { useState } from "react"
import { useTranslation } from "react-i18next"

import { cn } from "@/lib/utils"
import { Marker, MarkerContent, MarkerIcon } from "@/components/ui/marker"
import { BrainIcon } from "lucide-react"

const CLAMP_THRESHOLD = 240

/**
 * 思考过程：短的直接展示；长的默认折 4 行，点击展开/收起。
 * 推理内容是"按需查看"的辅助信息，不该吃掉正文的空间。
 */
export function ThoughtBlock({ content }: { content: string }) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const clampable = content.length > CLAMP_THRESHOLD

  return (
    <Marker>
      <MarkerIcon>
        <BrainIcon />
      </MarkerIcon>
      <MarkerContent>
        <div
          className={cn(
            "whitespace-pre-wrap",
            clampable && !expanded && "line-clamp-4"
          )}
        >
          {content}
        </div>
        {clampable ? (
          <button
            type="button"
            className="mt-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
            onClick={() => setExpanded((v) => !v)}
          >
            {expanded ? t("chat.collapse") : t("chat.expand")}
          </button>
        ) : null}
      </MarkerContent>
    </Marker>
  )
}
