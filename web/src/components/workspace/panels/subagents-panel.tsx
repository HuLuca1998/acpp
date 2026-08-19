import { memo, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { BotIcon, ChevronDownIcon, ChevronRightIcon } from "lucide-react"

import { PanelEmptyState } from "@/components/workspace/panels/panel-empty-state"
import { useChatPanel } from "@/components/workspace/chat-panel-context"
import { StatusDot, type StatusTone } from "@/components/status-dot"
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  collectSubagents,
  type SubagentEntry,
  type SubagentState,
} from "@/lib/subagents"

/** 状态 → 分组顺序与色调。进行中排最前，失败垫底但同样默认展开——它最该被看见。 */
const GROUPS: { state: SubagentState; tone: StatusTone }[] = [
  { state: "running", tone: "success" },
  { state: "done", tone: "muted" },
  { state: "failed", tone: "destructive" },
]

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
      <div className="text-[11px] font-medium tracking-wide text-muted-foreground/80">
        {label}
      </div>
      {value ? (
        <div className="max-h-56 overflow-y-auto rounded-md bg-background/60 px-2.5 py-2 text-xs leading-relaxed break-words whitespace-pre-wrap text-foreground">
          {value}
        </div>
      ) : (
        <div className="rounded-md border border-dashed border-border px-2.5 py-2 text-xs text-muted-foreground">
          {pending
            ? t("workspace.subagents.stillRunning")
            : t("workspace.subagents.unavailable")}
        </div>
      )}
    </div>
  )
}

/** 一条子代理：标题行常驻，明细在手风琴里——同时只摊开一条，列表不会被撑散。 */
function SubagentItem({
  entry,
  tone,
}: {
  entry: SubagentEntry
  tone: StatusTone
}) {
  const { t } = useTranslation()
  return (
    <AccordionItem
      value={entry.id}
      className="mb-1 rounded-md border-b-0 bg-muted/40 transition-colors last:mb-0 hover:bg-muted/60"
    >
      <AccordionTrigger className="items-center gap-2 px-2.5 py-2 hover:no-underline">
        <StatusDot tone={tone} pulse={entry.state === "running"} />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-medium text-foreground">
            {entry.name || t("workspace.subagents.unnamed")}
          </span>
          {entry.description ? (
            <span className="mt-0.5 block truncate text-xs font-normal text-muted-foreground">
              {entry.description}
            </span>
          ) : null}
        </span>
      </AccordionTrigger>
      <AccordionContent className="ml-3 space-y-2.5 border-l border-border pt-1 pb-3 pl-3">
        <Field label={t("workspace.subagents.input")} value={entry.input} />
        <Field
          label={t("workspace.subagents.output")}
          value={entry.output}
          pending={entry.state === "running"}
        />
      </AccordionContent>
    </AccordionItem>
  )
}

/** 一组状态。组本身可折叠，收起后连标题带内容一起让位给别的组。 */
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
    <section className="pb-1">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-1.5 px-2 py-1.5 text-left text-xs text-muted-foreground hover:text-foreground"
        aria-expanded={open}
      >
        <Chevron className="size-3.5" />
        <span>{t(`workspace.subagents.state.${state}`)}</span>
        <span className="ml-auto tabular-nums opacity-70">
          {entries.length}
        </span>
      </button>
      {open ? (
        <div className="px-1.5 pb-1">
          {entries.map((entry) => (
            <SubagentItem key={entry.id} entry={entry} tone={tone} />
          ))}
        </div>
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
      {/* 手风琴：明细一次只摊开一条，长输出不会把整个列表顶走。 */}
      <Accordion multiple={false}>
        {GROUPS.map(({ state, tone }) => (
          <Group
            key={state}
            state={state}
            tone={tone}
            entries={entries.filter((e) => e.state === state)}
          />
        ))}
      </Accordion>
    </ScrollArea>
  )
})

export const SubagentsPanelIcon = BotIcon
