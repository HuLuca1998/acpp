import { useTranslation } from "react-i18next"

import type { QueuedMessage } from "@/hooks/use-chat"
import { Hint } from "@/components/hint"
import {
  AtSignIcon,
  CornerDownRightIcon,
  ImageIcon,
  Undo2Icon,
} from "lucide-react"

/**
 * 一轮进行中用户插话的排队条：浮在输入卡上方。发出前有两个出口——
 * 「调整方向」立即插进正在跑的轮（steering），「撤回」回填输入框。
 */
export function QueuedMessages({
  items,
  onSteer,
  onRecall,
}: {
  items: QueuedMessage[]
  onSteer: (id: number) => void
  onRecall: (id: number) => void
}) {
  const { t } = useTranslation()
  if (items.length === 0) return null

  return (
    <div className="pointer-events-auto mb-2 flex flex-col gap-1.5">
      {items.map((q) => (
        <div
          key={q.id}
          className="flex items-center gap-2 rounded-xl border border-border/60 bg-card/90 py-2 pr-2 pl-3 shadow-md backdrop-blur-md transition-[opacity,translate] duration-150 ease-snappy starting:translate-y-1 starting:opacity-0 motion-reduce:starting:translate-y-0"
        >
          <CornerDownRightIcon className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="min-w-0 flex-1 truncate text-sm">
            {q.input.content}
          </span>
          {q.input.images?.length ? (
            <span className="flex shrink-0 items-center gap-1 text-xs text-muted-foreground tabular-nums">
              <ImageIcon className="size-3" />
              {q.input.images.length}
            </span>
          ) : null}
          {q.input.files?.length ? (
            <span className="flex shrink-0 items-center gap-1 text-xs text-muted-foreground tabular-nums">
              <AtSignIcon className="size-3" />
              {q.input.files.length}
            </span>
          ) : null}
          <Hint
            label={t("chat.queue.steer")}
            desc={t("chat.queue.steerDesc")}
            align="end"
          >
            <button
              type="button"
              className="flex shrink-0 items-center gap-1 rounded-md px-1.5 py-1 text-xs text-muted-foreground transition-[color,background-color,scale] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"
              onClick={() => onSteer(q.id)}
            >
              {t("chat.queue.steer")}
            </button>
          </Hint>
          <Hint
            label={t("chat.queue.recall")}
            desc={t("chat.queue.recallDesc")}
            align="end"
          >
            <button
              type="button"
              aria-label={t("chat.queue.recall")}
              className="flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-[color,background-color,scale] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"
              onClick={() => onRecall(q.id)}
            >
              <Undo2Icon className="size-3.5" />
            </button>
          </Hint>
        </div>
      ))}
    </div>
  )
}
