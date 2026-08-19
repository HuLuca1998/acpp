import { memo, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { BotIcon, ChevronDownIcon, ChevronRightIcon } from "lucide-react"

import { MarkdownContent } from "@/components/chat/markdown"
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

/**
 * 状态 → 分组顺序与色调。进行中排最前且**不给折叠**——正在跑的东西没有
 * 藏起来的道理；已完成与失败会越积越多，可以收。
 */
const GROUPS: {
  state: SubagentState
  tone: StatusTone
  collapsible: boolean
}[] = [
  { state: "running", tone: "success", collapsible: false },
  { state: "done", tone: "muted", collapsible: true },
  { state: "failed", tone: "destructive", collapsible: true },
]

/**
 * 输入/输出的展示格。两端能力不齐，取不到时如实说明，不装作空内容。
 * 内容按 markdown 渲染——子代理的简报与汇报本来就是 markdown 写的，
 * 纯文本铺出来的话代码块、列表、强调全糊成一片。
 */
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
        <div className="max-h-56 overflow-y-auto rounded-sm bg-background/60 px-2.5 py-2">
          <MarkdownContent className="prose-xs text-xs">
            {value}
          </MarkdownContent>
        </div>
      ) : (
        <div className="rounded-sm border border-dashed border-border px-2.5 py-2 text-xs text-muted-foreground">
          {pending
            ? t("workspace.subagents.stillRunning")
            : t("workspace.subagents.unavailable")}
        </div>
      )}
    </div>
  )
}

/**
 * 一条子代理：标题 + 元信息两行，明细在手风琴里——同时只摊开一条，
 * 长输出不会把列表顶散。展开箭头平时隐身，指到才出来。
 */
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
      className="group mb-0.5 rounded-sm border-b-0 bg-muted/40 transition-colors last:mb-0 hover:bg-muted/70"
    >
      <AccordionTrigger className="items-center gap-2 px-3 py-2 hover:no-underline **:data-[slot=accordion-trigger-icon]:opacity-0 group-hover:**:data-[slot=accordion-trigger-icon]:opacity-100 aria-expanded:**:data-[slot=accordion-trigger-icon]:opacity-100">
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-normal text-foreground">
            {entry.name || t("workspace.subagents.unnamed")}
          </span>
          <span className="mt-0.5 flex items-center gap-2 text-xs font-normal text-muted-foreground">
            <StatusDot
              tone={tone}
              pulse={entry.state === "running"}
              label={t(`workspace.subagents.state.${entry.state}`)}
            />
            {entry.description ? (
              <span className="truncate">{entry.description}</span>
            ) : null}
          </span>
        </span>
      </AccordionTrigger>
      <AccordionContent className="mx-3 space-y-2.5 border-t border-border/60 pt-2.5 pb-2.5">
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

/** 一组状态。计数紧跟标题，箭头收在末尾；不可折叠的组连箭头都不出。 */
function Group({
  state,
  tone,
  entries,
  collapsible,
}: {
  state: SubagentState
  tone: StatusTone
  entries: SubagentEntry[]
  collapsible: boolean
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(true)
  if (!entries.length) return null
  const shown = collapsible ? open : true
  const Chevron = shown ? ChevronDownIcon : ChevronRightIcon
  const heading = (
    <>
      <span>{t(`workspace.subagents.state.${state}`)}</span>
      <span className="tabular-nums opacity-60">{entries.length}</span>
      {collapsible ? <Chevron className="size-3.5 opacity-60" /> : null}
    </>
  )

  return (
    <section className="pb-2">
      {collapsible ? (
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          className="flex w-full items-center gap-1.5 px-3 py-1.5 text-left text-xs text-muted-foreground hover:text-foreground"
          aria-expanded={open}
        >
          {heading}
        </button>
      ) : (
        <div className="flex items-center gap-1.5 px-3 py-1.5 text-xs text-muted-foreground">
          {heading}
        </div>
      )}
      {shown ? (
        <div className="px-2">
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
      <Accordion multiple={false} className="py-1">
        {GROUPS.map(({ state, tone, collapsible }) => (
          <Group
            key={state}
            state={state}
            tone={tone}
            collapsible={collapsible}
            entries={entries.filter((e) => e.state === state)}
          />
        ))}
      </Accordion>
    </ScrollArea>
  )
})

export const SubagentsPanelIcon = BotIcon
