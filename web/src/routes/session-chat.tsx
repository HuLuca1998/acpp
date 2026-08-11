import { memo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useParams } from "react-router"

import { useChat, type LiveToolCall } from "@/hooks/use-chat"
import { useDraftSession } from "@/hooks/use-draft-session"
import type {
  AccessLevel,
  EffortLevel,
  ImageAttachment,
  Message,
  PendingElicitation,
  SessionSettings,
  SettingsPatch,
} from "@/types/acp"
import { fileToImageAttachment } from "@/lib/files"
import { cn } from "@/lib/utils"
import { formatDateTime } from "@/lib/format"
import { AttachmentTray } from "@/components/chat/attachment-tray"
import { Composer } from "@/components/chat/composer"
import { ComposerStatus } from "@/components/chat/composer-status"
import { CopyButton } from "@/components/chat/copy-button"
import { DirPicker } from "@/components/dir-picker"
import { DraftControls } from "@/components/chat/draft-controls"
import { PermissionCard } from "@/components/chat/permission-card"
import { PlanReviewCard } from "@/components/chat/plan-review-card"
import { MarkdownContent } from "@/components/chat/markdown"
import { PlanCard } from "@/components/chat/plan-card"
import { ThoughtBlock } from "@/components/chat/thought-block"
import { ToolCallBlock, type ToolCallPayload } from "@/components/chat/tool-call"
import {
  answerFor,
  parseElicitationSchema,
  type ElicitationSchema,
} from "@/lib/elicitation"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Bubble, BubbleContent } from "@/components/ui/bubble"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Marker, MarkerContent, MarkerIcon } from "@/components/ui/marker"
import { Message as MessageRow, MessageContent } from "@/components/ui/message"
import {
  MessageScroller,
  MessageScrollerButton,
  MessageScrollerContent,
  MessageScrollerItem,
  MessageScrollerProvider,
  MessageScrollerViewport,
} from "@/components/ui/message-scroller"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import {
  AtSignIcon,
  BrainIcon,
  ChevronRightIcon,
  CircleAlertIcon,
  CircleHelpIcon,
  FileIcon,
  ImageIcon,
  ListTodoIcon,
  MessagesSquareIcon,
  ShieldCheckIcon,
  ShieldIcon,
  SparklesIcon,
  WrenchIcon,
  ZapIcon,
} from "lucide-react"

/** 思考 / 工具调用等过程性消息，聚合成一个可折叠块展示。 */
const ACTIVITY_KINDS = new Set<Message["kind"]>([
  "thought",
  "tool_call",
  "tool_result",
  "permission_request",
  "plan",
])

type Block =
  | { type: "chat"; message: Message }
  | { type: "activity"; key: string; items: Message[] }

/** 把连续的过程性消息合并成一个 activity 块，正文消息原样保留。 */
function groupMessages(messages: Message[]): Block[] {
  const blocks: Block[] = []
  for (const message of messages) {
    if (!ACTIVITY_KINDS.has(message.kind)) {
      blocks.push({ type: "chat", message })
      continue
    }
    const last = blocks[blocks.length - 1]
    if (last?.type === "activity") {
      last.items.push(message)
    } else {
      blocks.push({
        type: "activity",
        key: `activity-${message.id}`,
        items: [message],
      })
    }
  }
  return blocks
}

export function SessionChat() {
  const { t } = useTranslation()
  const params = useParams()
  // /sessions/new 没有 :id —— 草稿态：会话未创建，首条消息落地才建。
  const isNew = params.id === undefined
  const sessionId = isNew ? 0 : Number(params.id)

  const chat = useChat(sessionId)
  const newSession = useDraftSession(isNew, t("sessions.form.defaultModel"))
  const [draft, setDraft] = useState("")

  // 待发送附件：图片（粘贴/选择）与 @ 引用的文件路径。
  const [images, setImages] = useState<ImageAttachment[]>([])
  const [files, setFiles] = useState<string[]>([])
  const [filePickerOpen, setFilePickerOpen] = useState(false)
  const [cwdPickerOpen, setCwdPickerOpen] = useState(false)
  const imageInputRef = useRef<HTMLInputElement>(null)

  // 草稿态显示待选工作目录（空则 agent 默认，再兜底 ~/acpp）。
  const draftCwd =
    newSession.cwd.trim() ||
    newSession.selectedAgent?.cwd ||
    t("sessions.form.cwdPlaceholder")

  async function addImages(picked: File[]) {
    const attachments = await Promise.all(picked.map(fileToImageAttachment))
    setImages((prev) => [...prev, ...attachments])
  }

  function submit() {
    const content = draft.trim()
    if (!content && images.length === 0 && files.length === 0) return
    const input = { content, images, files }
    if (isNew) {
      if (!newSession.selected || newSession.creating) return
      void newSession.start(input)
      return
    }
    // busy 时也允许发送：后端会把消息插进正在跑的轮（steering / 排队）。
    setDraft("")
    setImages([])
    setFiles([])
    void chat.send(input)
  }

  /** 空态建议芯片：草稿态直接开会话，老会话正常发。 */
  function sendSuggestion(text: string) {
    if (isNew) {
      void newSession.start({ content: text })
    } else {
      void chat.send({ content: text })
    }
  }

  if (chat.loading) {
    return (
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-3 p-4 lg:p-6">
        <Skeleton className="h-16 w-2/3" />
        <Skeleton className="h-16 w-1/2 self-end" />
        <Skeleton className="h-16 w-3/4" />
      </div>
    )
  }

  // 会话已被删除或 id 无效：给明确的出口，而不是报"连不上 agent"。
  if (chat.notFound) {
    return (
      <Empty className="h-full justify-center">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <MessagesSquareIcon />
          </EmptyMedia>
          <EmptyTitle>{t("errors.sessionNotFound")}</EmptyTitle>
          <EmptyDescription>
            {t("errors.sessionNotFoundHint")}
          </EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button render={<Link to="/sessions" />}>
            {t("errors.backToSessions")}
          </Button>
        </EmptyContent>
      </Empty>
    )
  }

  const liveActivityCount =
    (chat.streamingThought ? 1 : 0) +
    chat.liveTools.length +
    chat.permissions.length
  const hasContent =
    chat.messages.length > 0 ||
    chat.streamingText !== "" ||
    liveActivityCount > 0 ||
    (chat.plan?.length ?? 0) > 0

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      {/* 顶部信息条：连接状态 + 会话信息，保持轻量；草稿态没有会话可显示。 */}
      <div className="mx-auto w-full max-w-3xl px-4 pt-3 lg:px-6">
        {!isNew ? (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <span
              aria-label={
                chat.connected ? t("chat.connected") : t("chat.disconnected")
              }
              className={cn(
                "size-2 shrink-0 rounded-full",
                chat.connected ? "bg-success" : "bg-destructive",
                chat.connected &&
                  chat.busy &&
                  "animate-breathe motion-reduce:animate-none"
              )}
            />
            {chat.session ? (
              <>
                <span className="shrink-0 font-medium text-foreground">
                  {chat.session.title}
                </span>
                <span className="shrink-0">{chat.session.agentName}</span>
              </>
            ) : null}
          </div>
        ) : null}

        {chat.error || newSession.error ? (
          <Alert variant="destructive" className="mt-2">
            <AlertTitle>{t("errors.openFailed")}</AlertTitle>
            <AlertDescription>{chat.error ?? newSession.error}</AlertDescription>
          </Alert>
        ) : null}

      </div>

      {/* 消息流：底部 padding 给悬浮输入让位。 */}
      <div className="min-h-0 flex-1 overflow-hidden">
        {!hasContent ? (
          <Empty className="h-full justify-center">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <MessagesSquareIcon />
              </EmptyMedia>
              <EmptyTitle>{t("chat.empty")}</EmptyTitle>
              <EmptyDescription>{t("chat.emptyHint")}</EmptyDescription>
            </EmptyHeader>
            {/* 建议芯片：给第一句一个起点，点击即发送。 */}
            <EmptyContent>
              <div className="flex flex-wrap justify-center gap-2">
                {(["intro", "changes", "help"] as const).map((key) => (
                  <Button
                    key={key}
                    variant="outline"
                    size="sm"
                    className="rounded-full"
                    disabled={
                      isNew
                        ? newSession.creating || !newSession.selected
                        : chat.busy
                    }
                    onClick={() => sendSuggestion(t(`chat.suggestions.${key}`))}
                  >
                    {t(`chat.suggestions.${key}`)}
                  </Button>
                ))}
              </div>
            </EmptyContent>
          </Empty>
        ) : (
          <MessageScrollerProvider>
            <MessageScroller>
              <MessageScrollerViewport>
                <MessageScrollerContent className="mx-auto w-full max-w-3xl px-4 pt-4 pb-48 lg:px-6">
                  {chat.hasEarlier ? (
                    <MessageScrollerItem scrollAnchor={false}>
                      <div className="flex justify-center">
                        <Button
                          variant="ghost"
                          size="sm"
                          className="text-muted-foreground"
                          onClick={() => void chat.loadEarlier()}
                        >
                          {t("chat.loadEarlier")}
                        </Button>
                      </div>
                    </MessageScrollerItem>
                  ) : null}
                  {groupMessages(chat.messages).map((block) =>
                    block.type === "chat" ? (
                      <MessageScrollerItem
                        key={block.message.id}
                        messageId={String(block.message.id)}
                        scrollAnchor={block.message.role === "user"}
                      >
                        <ChatMessage message={block.message} />
                      </MessageScrollerItem>
                    ) : (
                      <MessageScrollerItem key={block.key} scrollAnchor={false}>
                        <ActivitySection count={block.items.length}>
                          {block.items.map((item) => (
                            <ActivityMessage key={item.id} message={item} />
                          ))}
                        </ActivitySection>
                      </MessageScrollerItem>
                    )
                  )}

                  {/* 任务计划：随 plan 事件实时更新，轮次结束保留最终状态。 */}
                  {chat.plan && chat.plan.length > 0 ? (
                    <MessageScrollerItem scrollAnchor={false}>
                      <PlanCard entries={chat.plan} />
                    </MessageScrollerItem>
                  ) : null}

                  {liveActivityCount > 0 ? (
                    <MessageScrollerItem scrollAnchor={false}>
                      <ActivitySection
                        count={liveActivityCount}
                        busy={chat.busy}
                      >
                        {chat.streamingThought ? (
                          <Marker>
                            <MarkerIcon>
                              <BrainIcon />
                            </MarkerIcon>
                            <MarkerContent>
                              <span className="text-shimmer">
                                {t("chat.thinking")}
                              </span>
                              {/* 只看思考的"最新进展"：截尾 + 行数钳制，不淹没界面。 */}
                              <div className="mt-1 line-clamp-3 whitespace-pre-wrap">
                                {chat.streamingThought.slice(-600)}
                              </div>
                            </MarkerContent>
                          </Marker>
                        ) : null}
                        {chat.permissions.map((perm) => (
                          <Marker key={perm.id}>
                            <MarkerIcon>
                              <ShieldCheckIcon />
                            </MarkerIcon>
                            <MarkerContent>
                              {t("chat.permission.resolved", {
                                title: perm.title,
                                choice:
                                  perm.choice || t("chat.permission.cancelled"),
                              })}
                            </MarkerContent>
                          </Marker>
                        ))}
                        {chat.liveTools.map((tool) => (
                          <LiveToolMarker key={tool.id} tool={tool} />
                        ))}
                      </ActivitySection>
                    </MessageScrollerItem>
                  ) : null}

                  {chat.streamingText ? (
                    <MessageScrollerItem scrollAnchor={false}>
                      <MarkdownContent>{chat.streamingText}</MarkdownContent>
                    </MessageScrollerItem>
                  ) : null}

                  {chat.permission ? (
                    <MessageScrollerItem
                      key={chat.permission.id}
                      scrollAnchor={false}
                    >
                      {chat.permission.planReview ? (
                        <PlanReviewCard
                          permission={chat.permission}
                          onResolve={(optionId, choiceName) =>
                            void chat.resolvePermission(
                              chat.permission!.id,
                              optionId,
                              choiceName
                            )
                          }
                        />
                      ) : (
                        <PermissionCard
                          permission={chat.permission}
                          onResolve={(optionId, choiceName) =>
                            void chat.resolvePermission(
                              chat.permission!.id,
                              optionId,
                              choiceName
                            )
                          }
                        />
                      )}
                    </MessageScrollerItem>
                  ) : null}

                  {chat.elicitation ? (
                    <MessageScrollerItem
                      key={chat.elicitation.id}
                      scrollAnchor={false}
                    >
                      <ElicitationCard
                        elicitation={chat.elicitation}
                        onResolve={(action, content) =>
                          void chat.resolveElicitation(
                            chat.elicitation!.id,
                            action,
                            content
                          )
                        }
                      />
                    </MessageScrollerItem>
                  ) : null}

                  {chat.busy &&
                  !chat.streamingText &&
                  liveActivityCount === 0 &&
                  !chat.elicitation &&
                  !chat.permission ? (
                    <MessageScrollerItem scrollAnchor={false}>
                      <Marker role="status">
                        <MarkerIcon>
                          <Spinner />
                        </MarkerIcon>
                        <MarkerContent className="text-shimmer">
                          {t("chat.thinking")}
                        </MarkerContent>
                      </Marker>
                    </MessageScrollerItem>
                  ) : null}

                  {/* 非正常结束原因内联在消息流末尾，紧跟被截断的回答。 */}
                  {chat.stopReason ? (
                    <MessageScrollerItem scrollAnchor={false}>
                      <Marker>
                        <MarkerIcon>
                          <CircleAlertIcon className="text-warning" />
                        </MarkerIcon>
                        <MarkerContent>
                          {t(`chat.stopReason.${chat.stopReason}` as never, {
                            defaultValue: chat.stopReason,
                          })}
                        </MarkerContent>
                      </Marker>
                    </MessageScrollerItem>
                  ) : null}
                </MessageScrollerContent>
              </MessageScrollerViewport>
              <MessageScrollerButton className="data-[direction=end]:bottom-40" />
            </MessageScroller>
          </MessageScrollerProvider>
        )}
      </div>

      <Composer
        value={draft}
        onChange={setDraft}
        onSubmit={submit}
        onCancel={isNew ? undefined : () => void chat.cancel()}
        busy={chat.busy}
        pending={newSession.creating}
        disabled={isNew && (newSession.agents === null || !newSession.selected)}
        placeholder={t("chat.placeholder")}
        commands={
          isNew
            ? (newSession.selectedAgent?.commands ?? []).filter(
                (c) => !c.disabled
              )
            : chat.commands
        }
        attachments={
          <AttachmentTray
            images={images}
            files={files}
            onRemoveImage={(i) =>
              setImages((prev) => prev.filter((_, idx) => idx !== i))
            }
            onRemoveFile={(i) =>
              setFiles((prev) => prev.filter((_, idx) => idx !== i))
            }
          />
        }
        onPasteImages={(picked) => void addImages(picked)}
        footer={
          <ComposerStatus
            cwd={isNew ? draftCwd : chat.session?.cwd}
            gitBranch={isNew ? undefined : chat.session?.gitBranch}
            usage={isNew ? null : chat.contextUsage}
            onPickCwd={isNew ? () => setCwdPickerOpen(true) : undefined}
          />
        }
      >
        {isNew ? (
          <DraftControls draft={newSession} />
        ) : (
          <SettingsSelectors
            settings={chat.settings}
            disabled={chat.busy}
            onApply={chat.applySettings}
          />
        )}

        {/* 附件：图片上传与 @ 文件引用，两种态都可用。 */}
        <button
          type="button"
          aria-label={t("chat.attachments.image")}
          className="flex size-7 items-center justify-center rounded-full text-muted-foreground transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"
          onClick={() => imageInputRef.current?.click()}
        >
          <ImageIcon className="size-3.5" />
        </button>
        <button
          type="button"
          aria-label={t("chat.attachments.file")}
          className="flex size-7 items-center justify-center rounded-full text-muted-foreground transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"
          onClick={() => setFilePickerOpen(true)}
        >
          <AtSignIcon className="size-3.5" />
        </button>
      </Composer>

      <input
        ref={imageInputRef}
        type="file"
        accept="image/*"
        multiple
        className="hidden"
        onChange={(e) => {
          const picked = Array.from(e.target.files ?? [])
          e.target.value = ""
          if (picked.length > 0) void addImages(picked)
        }}
      />

      <DirPicker
        mode="file"
        open={filePickerOpen}
        onOpenChange={setFilePickerOpen}
        initialPath={
          (isNew ? newSession.cwd.trim() : chat.session?.cwd) || undefined
        }
        onSelect={(path) =>
          setFiles((prev) => (prev.includes(path) ? prev : [...prev, path]))
        }
      />

      {/* 草稿态：点状态栏里的工作目录换目录。 */}
      <DirPicker
        open={cwdPickerOpen}
        onOpenChange={setCwdPickerOpen}
        initialPath={newSession.cwd.trim() || newSession.selectedAgent?.cwd || undefined}
        onSelect={newSession.setCwd}
      />
    </div>
  )
}

/**
 * 统一设置选择器：模型 / 思考深度 / 权限档三个下拉 + plan / fast 两个开关。
 * 只认后端的统一 Settings 视图，不出现任何 runtime 特有的字面量；
 * 空数组 / 不支持的维度直接不渲染对应控件。
 */
function SettingsSelectors({
  settings,
  disabled,
  onApply,
}: {
  settings: SessionSettings | null
  disabled: boolean
  onApply: (patch: SettingsPatch) => Promise<void>
}) {
  const { t } = useTranslation()
  if (!settings) return null

  return (
    <>
      {settings.models.length > 0 ? (
        <CapSelect
          icon={<SparklesIcon className="size-3.5" />}
          value={settings.currentModel ?? ""}
          placeholder={t("chat.settings.model")}
          // 只显示模型名：带描述的双行条目会把长清单撑到遮挡。
          options={settings.models.map((m) => ({
            value: m.id,
            name: m.name,
          }))}
          disabled={disabled}
          onChange={(v) => void onApply({ model: v })}
        />
      ) : null}

      {settings.efforts.length > 0 ? (
        <CapSelect
          icon={<BrainIcon className="size-3.5" />}
          value={settings.currentEffort ?? ""}
          placeholder={t("chat.settings.effortLabel")}
          options={settings.efforts.map((e) => ({
            value: e,
            name: t(`chat.settings.effort.${e}`),
          }))}
          disabled={disabled}
          onChange={(v) => void onApply({ effort: v as EffortLevel })}
        />
      ) : null}

      {settings.levels.length > 0 ? (
        <CapSelect
          icon={<ShieldIcon className="size-3.5" />}
          value={settings.currentLevel ?? ""}
          placeholder={t("chat.settings.levelLabel")}
          options={settings.levels.map((l) => ({
            value: l,
            name: t(`chat.settings.level.${l}`),
            description: t(`chat.settings.levelDesc.${l}`),
          }))}
          disabled={disabled}
          onChange={(v) => void onApply({ level: v as AccessLevel })}
        />
      ) : null}

      {settings.planSupported ? (
        <SettingToggle
          icon={<ListTodoIcon className="size-3.5" />}
          label={t("chat.settings.plan")}
          on={settings.planOn}
          disabled={disabled}
          onToggle={(on) => void onApply({ plan: on })}
        />
      ) : null}

      {settings.fastSupported ? (
        <SettingToggle
          icon={<ZapIcon className="size-3.5" />}
          label={t("chat.settings.fast")}
          on={settings.fastOn}
          disabled={disabled}
          onToggle={(on) => void onApply({ fast: on })}
        />
      ) : null}
    </>
  )
}

/** 胶囊式开关：开启时主色浸染，与选择器同排使用。 */
function SettingToggle({
  icon,
  label,
  on,
  disabled,
  onToggle,
}: {
  icon: React.ReactNode
  label: string
  on: boolean
  disabled: boolean
  onToggle: (on: boolean) => void
}) {
  return (
    <button
      type="button"
      aria-pressed={on}
      disabled={disabled}
      onClick={() => onToggle(!on)}
      className={cn(
        "flex h-7 items-center gap-1 rounded-full px-2.5 text-xs transition-[scale,background-color,color] duration-150 ease-snappy active:scale-[0.97] disabled:pointer-events-none disabled:opacity-50",
        on
          ? "bg-primary/15 text-primary"
          : "text-muted-foreground hover:bg-muted hover:text-foreground"
      )}
    >
      {icon}
      {label}
    </button>
  )
}

function CapSelect({
  icon,
  value,
  placeholder,
  options,
  disabled,
  onChange,
}: {
  icon?: React.ReactNode
  value: string
  /** 当前值不在选项里（如 runtime 默认态）时显示的占位文案。 */
  placeholder?: string
  options: { value: string; name: string; description?: string }[]
  disabled: boolean
  onChange: (value: string) => void
}) {
  const current = options.find((o) => o.value === value)

  return (
    <Select
      value={value}
      onValueChange={(v) => {
        if (typeof v === "string" && v && v !== value) onChange(v)
      }}
    >
      <SelectTrigger
        size="sm"
        disabled={disabled}
        className="h-7 gap-1 rounded-full border-transparent bg-transparent px-2.5 text-xs text-muted-foreground shadow-none transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97] dark:bg-transparent dark:hover:bg-muted"
      >
        {icon}
        <SelectValue>{current?.name ?? (value || placeholder)}</SelectValue>
      </SelectTrigger>
      {/* 输入框贴底，向上弹出才不被视口截断；关掉选中项对齐的覆盖式定位，
          并放宽默认的可用高度限制，短清单一屏放完不内滚。 */}
      <SelectContent side="top" alignItemWithTrigger={false} className="max-h-96">
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            <div className="flex min-w-0 flex-col">
              <span>{o.name}</span>
              {o.description ? (
                <span className="max-w-64 truncate text-xs text-muted-foreground">
                  {o.description}
                </span>
              ) : null}
            </div>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

/**
 * agent 的交互式提问卡片：每道题渲染选项按钮组，可附自由输入；
 * 选选项和填「其他」互斥。提交走 accept，跳过走 decline。
 */
function ElicitationCard({
  elicitation,
  onResolve,
}: {
  elicitation: PendingElicitation
  onResolve: (
    action: "accept" | "decline",
    content?: Record<string, string>
  ) => void
}) {
  const { t } = useTranslation()
  const [answers, setAnswers] = useState<Record<string, string>>({})
  const [others, setOthers] = useState<Record<string, string>>({})
  const [step, setStep] = useState(0)
  const [submitted, setSubmitted] = useState(false)

  type Answers = Record<string, string>

  const questions = elicitation.questions
  const total = questions.length
  const q = questions[Math.min(step, total - 1)]
  const isLast = step >= total - 1

  const isAnswered = (
    question: PendingElicitation["questions"][number],
    a: Answers,
    o: Answers
  ) => Boolean(a[question.id]) || (o[question.id]?.trim() ?? "") !== ""

  const isComplete = (a: Answers, o: Answers) =>
    questions.some((question) => isAnswered(question, a, o)) &&
    questions.every(
      (question) => !question.required || isAnswered(question, a, o)
    )

  function submitWith(a: Answers, o: Answers) {
    const content: Record<string, string> = {}
    for (const question of questions) {
      const other = o[question.id]?.trim() ?? ""
      if (other !== "") {
        // 纯自由输入题没有独立的 other 字段，答案直接写回题目本身。
        content[question.otherFieldId ?? question.id] = other
        continue
      }
      if (a[question.id]) content[question.id] = a[question.id]
    }
    setSubmitted(true)
    onResolve("accept", content)
  }

  /** 记录答案后前进：非末题翻页，末题在必答齐时立即提交。 */
  function advance(a: Answers, o: Answers) {
    if (!isLast) {
      setStep(step + 1)
      return
    }
    if (isComplete(a, o)) submitWith(a, o)
  }

  function pickOption(value: string) {
    const nextAnswers = { ...answers, [q.id]: value }
    const nextOthers = { ...others, [q.id]: "" }
    setAnswers(nextAnswers)
    setOthers(nextOthers)
    advance(nextAnswers, nextOthers)
  }

  const currentAnswered = isAnswered(q, answers, others)
  const canFinish = isComplete(answers, others)

  return (
    <div className="rounded-xl border border-border bg-card p-4 shadow-sm">
      <div className="mb-3 flex items-center gap-2 text-sm font-medium">
        <CircleHelpIcon className="size-4 text-primary" />
        {t("chat.elicitation.title")}
        {total > 1 ? (
          <span className="ml-auto text-xs font-normal text-muted-foreground">
            {step + 1} / {total}
          </span>
        ) : null}
      </div>

      {/* key 换题触发 remount，@starting-style 做轻快的换页淡入。 */}
      <div
        key={q.id}
        className="flex flex-col gap-2 transition-[opacity,translate] duration-200 ease-snappy starting:translate-x-1.5 starting:opacity-0 motion-reduce:starting:translate-x-0"
      >
        <div>
          <div className="text-sm font-medium">{q.title}</div>
          {q.description ? (
            <div className="text-sm text-muted-foreground">{q.description}</div>
          ) : null}
        </div>

        {q.options.length > 0 ? (
          <div className="flex flex-wrap gap-1.5">
            {q.options.map((option) => (
              <Button
                key={option.value}
                type="button"
                size="sm"
                variant={answers[q.id] === option.value ? "default" : "outline"}
                className="h-7 rounded-full text-xs"
                title={option.description}
                disabled={submitted}
                onClick={() => pickOption(option.value)}
              >
                {option.value}
              </Button>
            ))}
          </div>
        ) : null}

        {q.otherFieldId || q.options.length === 0 ? (
          <Input
            value={others[q.id] ?? ""}
            placeholder={t("chat.elicitation.otherPlaceholder")}
            className="h-8 text-sm"
            disabled={submitted}
            onChange={(e) => {
              const value = e.target.value
              setOthers((prev) => ({ ...prev, [q.id]: value }))
              if (value.trim() !== "") {
                setAnswers((prev) => ({ ...prev, [q.id]: "" }))
              }
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" && (others[q.id]?.trim() ?? "") !== "") {
                e.preventDefault()
                advance(answers, others)
              }
            }}
          />
        ) : null}
      </div>

      <div className="mt-4 flex items-center gap-2">
        {step > 0 ? (
          <Button
            size="sm"
            variant="ghost"
            disabled={submitted}
            onClick={() => setStep(step - 1)}
          >
            {t("chat.elicitation.back")}
          </Button>
        ) : null}
        {!isLast ? (
          <Button
            size="sm"
            variant="outline"
            disabled={submitted || (q.required && !currentAnswered)}
            onClick={() => setStep(step + 1)}
          >
            {t("chat.elicitation.next")}
          </Button>
        ) : (
          <Button
            size="sm"
            disabled={!canFinish || submitted}
            onClick={() => submitWith(answers, others)}
          >
            {t("chat.elicitation.submit")}
          </Button>
        )}
        <Button
          size="sm"
          variant="ghost"
          className="ml-auto text-muted-foreground"
          disabled={submitted}
          onClick={() => {
            setSubmitted(true)
            onResolve("decline")
          }}
        >
          {t("chat.elicitation.skip")}
        </Button>
      </div>
    </div>
  )
}

/** 可折叠的过程块：默认收起，标题行显示条数；busy 时带 spinner。 */
function ActivitySection({
  count,
  busy = false,
  children,
}: {
  count: number
  busy?: boolean
  children: React.ReactNode
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  return (
    <div className="flex flex-col gap-2">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="flex w-fit items-center gap-2 rounded-md text-sm text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        <ChevronRightIcon
          className={cn("size-4 transition-transform", open && "rotate-90")}
        />
        {busy ? (
          <Spinner className="size-4" />
        ) : (
          <WrenchIcon className="size-4" />
        )}
        <span>{t("chat.activity")}</span>
        <Badge variant="secondary">{count}</Badge>
      </button>
      {open ? (
        <div className="ml-2 flex flex-col gap-2 border-l border-border pl-4 transition-[opacity,translate] duration-200 ease-snappy starting:-translate-y-0.5 starting:opacity-0 motion-reduce:starting:translate-y-0">
          {children}
        </div>
      ) : null}
    </div>
  )
}

function LiveToolMarker({ tool }: { tool: LiveToolCall }) {
  return (
    <ToolCallBlock
      title={tool.title}
      status={tool.status}
      payload={{
        kind: tool.kind,
        rawInput: tool.rawInput as ToolCallPayload["rawInput"],
        rawOutput: tool.rawOutput as ToolCallPayload["rawOutput"],
        content: tool.content as ToolCallPayload["content"],
      }}
    />
  )
}

/** activity 块内的单条过程消息。memo 理由同 ChatMessage。 */
const ActivityMessage = memo(function ActivityMessage({
  message,
}: {
  message: Message
}) {
  if (message.kind === "thought") {
    return <ThoughtBlock content={message.content} />
  }

  if (message.kind === "permission_request") {
    return (
      <Marker>
        <MarkerIcon>
          <ShieldCheckIcon />
        </MarkerIcon>
        <MarkerContent>{message.content}</MarkerContent>
      </Marker>
    )
  }

  const payload = (message.payload ?? null) as ToolCallPayload | null
  return (
    <ToolCallBlock
      title={message.content}
      status={payload?.status}
      payload={payload}
    />
  )
})

/** 正文消息：用户消息用主色气泡靠右，agent 输出通栏渲染 markdown。
 *  memo 是必须的——流式期间每个 chunk 都触发页面重渲染，
 *  没有它整份历史的 markdown 会一遍遍重新解析。 */
const ChatMessage = memo(function ChatMessage({ message }: { message: Message }) {
  const { i18n } = useTranslation()
  const timestamp = formatDateTime(message.createdAt, i18n.language)

  if (message.kind === "elicitation") {
    return <ElicitationAnsweredCard message={message} />
  }

  if (message.role === "user") {
    // 附件（图片 / @ 引用文件）由发送入参或转录重建带回 payload。
    const payload = message.payload as {
      images?: { data: string; mimeType: string }[]
      files?: string[]
    } | null
    return (
      <MessageRow align="end">
        <MessageContent>
          <div
            className="group/msg flex items-center justify-end gap-1.5"
            title={timestamp}
          >
            <CopyButton
              text={message.content}
              className="opacity-0 transition-opacity duration-150 group-hover/msg:opacity-100 focus-visible:opacity-100"
            />
            <div className="flex flex-col items-end gap-1.5">
              {payload?.images?.length ? (
                <div className="flex flex-wrap justify-end gap-1.5">
                  {payload.images.map((img, index) => (
                    <img
                      key={index}
                      src={`data:${img.mimeType};base64,${img.data}`}
                      alt=""
                      className="max-h-40 max-w-60 rounded-lg border border-border object-contain"
                    />
                  ))}
                </div>
              ) : null}
              {payload?.files?.length ? (
                <div className="flex flex-wrap justify-end gap-1.5">
                  {payload.files.map((path) => (
                    <span
                      key={path}
                      title={path}
                      className="flex h-6 items-center gap-1 rounded-full border border-border px-2 text-xs text-muted-foreground"
                    >
                      <FileIcon className="size-3" />
                      <span className="max-w-40 truncate font-mono">
                        {path.split("/").pop()}
                      </span>
                    </span>
                  ))}
                </div>
              ) : null}
              {message.content ? (
                <Bubble variant="default" align="end">
                  <BubbleContent className="whitespace-pre-wrap">
                    {message.content}
                  </BubbleContent>
                </Bubble>
              ) : null}
            </div>
          </div>
        </MessageContent>
      </MessageRow>
    )
  }

  return (
    <div className="group/msg" title={timestamp}>
      <MarkdownContent>{message.content}</MarkdownContent>
      {/* 操作行占位固定高度，浮现时不推挤下方内容。 */}
      <div className="mt-1 flex h-6 items-center gap-1 opacity-0 transition-opacity duration-150 group-hover/msg:opacity-100 focus-within:opacity-100">
        <CopyButton text={message.content} />
      </div>
    </div>
  )
})

/** 已完成的交互式提问：每题显示全部选项，标出用户的选择，可随时回看。 */
function ElicitationAnsweredCard({ message }: { message: Message }) {
  const { t } = useTranslation()
  const payload = message.payload as {
    action?: string
    schema?: unknown
    answers?: Record<string, unknown>
  } | null

  const questions = parseElicitationSchema(
    (payload?.schema ?? null) as ElicitationSchema | null
  )
  const accepted = payload?.action === "accept"

  return (
    <div className="rounded-xl border border-border bg-card p-4 shadow-sm">
      <div className="mb-3 flex items-center gap-2 text-sm font-medium">
        <CircleHelpIcon className="size-4 text-primary" />
        {t("chat.elicitation.title")}
        {!accepted ? (
          <Badge variant="secondary" className="ml-auto">
            {t("chat.elicitation.skipped")}
          </Badge>
        ) : null}
      </div>

      {questions.length === 0 ? (
        <div className="text-sm text-muted-foreground">{message.content}</div>
      ) : (
        <div className="flex flex-col gap-3">
          {questions.map((q) => {
            const answer = answerFor(q, payload?.answers)
            const isCustom =
              answer !== "" && !q.options.some((o) => o.value === answer)
            return (
              <div key={q.id} className="flex flex-col gap-1.5">
                <div>
                  <div className="text-sm font-medium">{q.title}</div>
                  {q.description ? (
                    <div className="text-sm text-muted-foreground">
                      {q.description}
                    </div>
                  ) : null}
                </div>
                <div className="flex flex-wrap gap-1.5">
                  {q.options.map((option) => (
                    <span
                      key={option.value}
                      title={option.description}
                      className={cn(
                        "inline-flex h-7 items-center rounded-full border px-2.5 text-xs",
                        option.value === answer
                          ? "border-primary bg-primary text-primary-foreground"
                          : "border-border text-muted-foreground/70"
                      )}
                    >
                      {option.value}
                    </span>
                  ))}
                  {isCustom ? (
                    <span className="inline-flex h-7 items-center rounded-full border border-primary bg-primary px-2.5 text-xs text-primary-foreground">
                      {answer}
                    </span>
                  ) : null}
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
