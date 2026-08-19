import { memo, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { BotIcon, ChevronDownIcon, ChevronRightIcon } from "lucide-react"

import { PanelEmptyState } from "@/components/workspace/panels/panel-empty-state"
import { useChatPanel } from "@/components/workspace/chat-panel-context"
import { StatusDot, type StatusTone } from "@/components/status-dot"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  collectSubagents,
  type SubagentEntry,
  type SubagentState,
} from "@/lib/subagents"
import { cn } from "@/lib/utils"

/** 状态 → 分组顺序与色调。进行中排最前，失败垫底但默认也展开——它最该被看见。 */
const GROUPS: { state: SubagentState; tone: StatusTone }[] = [
  { state: "running", tone: "success" },
  { state: "done", tone: "muted" },
  { state: "failed", tone: "destructive" },
]

function SubagentRow({
  entry,
  tone,
}: {
  entry: SubagentEntry
  tone: StatusTone
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const Chevron = open ? ChevronDownIcon : ChevronRightIcon

  return (
    <li>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-accent"
        aria-expanded={open}
      >
        <Chevron className="size-4 shrink-0 text-muted-foreground" />
        <StatusDot tone={tone} pulse={entry.state === "running"} />
        <span className="truncate text-sm">
          {entry.name || t("workspace.subagents.unnamed")}
        </span>
        {entry.description ? (
          <span className="truncate text-xs text-muted-foreground">
            {entry.description}
          </span>
        ) : null}
      </button>

      {open ? (
        <div className="space-y-3 px-8 pt-1 pb-3">
          <Field label={t("workspace.subagents.input")} value={entry.input} />
          <Field
            label={t("workspace.subagents.output")}
            value={entry.output}
            pending={entry.state === "running"}
          />
        </div>
      ) : null}
    </li>
  )
}

/** 输入/输出的展示格。两端能力不齐，取不到时如实说明，不装作空内容。 */
function Field({
  label,
  value,
  pending = false,
}: {
  label: string
  value: string
  pending?: boolean
}) {
  const { t } = useTranslation()
  return (
    <div className="space-y-1">
      <div className="text-xs font-medium text-muted-foreground">{label}</div>
      {value ? (
        <p className="text-xs break-words whitespace-pre-wrap text-foreground">
          {value}
        </p>
      ) : (
        <p className="text-xs text-muted-foreground">
          {pending
            ? t("workspace.subagents.stillRunning")
            : t("workspace.subagents.unavailable")}
        </p>
      )}
    </div>
  )
}

function Group({
  state,
  tone,
  entries,
}: {
  state: SubagentState
  tone: StatusTone
  entries: SubagentEntry[]
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(true)
  if (!entries.length) return null
  const Chevron = open ? ChevronDownIcon : ChevronRightIcon

  return (
    <section>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-1.5 px-2 py-1 text-left text-xs font-medium text-muted-foreground hover:text-foreground"
        aria-expanded={open}
      >
        <Chevron className="size-3.5" />
        <span>{t(`workspace.subagents.state.${state}`)}</span>
        <span className="tabular-nums">{entries.length}</span>
      </button>
      {open ? (
        <ul className={cn("pb-1")}>
          {entries.map((entry) => (
            <SubagentRow key={entry.id} entry={entry} tone={tone} />
          ))}
        </ul>
      ) : null}
    </section>
  )
}

/**
 * 子代理面板：把 agent 自己派出去的活收拢成一份清单。
 *
 * 只展示「派了什么、拿回什么」——中间过程刻意不进这里，它们已经被主对话流
 * 摘掉了。两端能拿到的东西不一样（codex 的任务简报是密文），缺的格子如实说明。
 */
export const SubagentsPanel = memo(function SubagentsPanel() {
  const { t } = useTranslation()
  const { chat } = useChatPanel()
  const entries = useMemo(
    () => collectSubagents(chat.messages, chat.liveTools),
    [chat.messages, chat.liveTools]
  )

  if (!entries.length) {
    return (
      <PanelEmptyState
        title={t("workspace.subagents.emptyTitle")}
        description={t("workspace.subagents.emptyHint")}
      />
    )
  }

  return (
    <ScrollArea className="h-full">
      <div className="py-1">
        {GROUPS.map(({ state, tone }) => (
          <Group
            key={state}
            state={state}
            tone={tone}
            entries={entries.filter((e) => e.state === state)}
          />
        ))}
      </div>
    </ScrollArea>
  )
})

export const SubagentsPanelIcon = BotIcon
