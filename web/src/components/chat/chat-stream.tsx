import { memo, useEffect, useMemo, useRef } from "react"
import { useTranslation } from "react-i18next"

import {
  ActivitySection,
  LiveToolMarker,
} from "@/components/chat/chat-messages"
import { ChatHistory } from "@/components/chat/chat-history"
import { ElicitationCard } from "@/components/chat/cards/elicitation-card"
import { FileEditCard } from "@/components/chat/file-edit-card"
import { StreamingMarkdown } from "@/components/chat/markdown"
import { MessageIndex } from "@/components/chat/message-index"
import { AgentAvatar, AgentRow } from "@/components/chat/message-shell"
import { PermissionCard } from "@/components/chat/cards/permission-card"
import { PlanCard } from "@/components/chat/plan-card"
import { PlanReviewCard } from "@/components/chat/cards/plan-review-card"
import { TouchedFile } from "@/components/chat/touched-file"
import type { ToolCallPayload } from "@/components/chat/tool-call"
import { Marker, MarkerContent, MarkerIcon } from "@/components/ui/marker"
import {
  MessageScroller,
  MessageScrollerButton,
  MessageScrollerContent,
  MessageScrollerItem,
  MessageScrollerProvider,
  MessageScrollerViewport,
} from "@/components/ui/message-scroller"
import { Spinner } from "@/components/ui/spinner"
import type { ChatState } from "@/lib/chat/chat-events"
import { groupMessages, turnStartsOf } from "@/lib/chat/message-blocks"
import { BrainIcon, CircleAlertIcon, ShieldCheckIcon } from "lucide-react"

/**
 * ChatStream 消费的数据源：聊天状态 + 三个交互回调。结构接口而不是
 * 绑死 useChat 的返回值——任何返回结构兼容形状的流状态机都能渲染。
 */
export interface ChatStreamSource extends ChatState {
  /** 拉一页更早的消息；返回有没有拿到新内容（哨兵的泵取循环靠它停下）。 */
  loadEarlier: () => Promise<boolean>
  /** 重跑最后一条用户消息（出错后的补救入口）。 */
  retry: () => Promise<void> | void
  resolvePermission: (
    id: string,
    optionId: string,
    choiceName: string
  ) => Promise<void> | void
  resolveElicitation: (
    id: string,
    action: "accept" | "decline" | "cancel",
    content?: Record<string, string>
  ) => Promise<void> | void
}

/**
 * 消息流：历史消息块 + 计划卡 + 实时活动区（思考/工具/权限）+ 流式正文
 * + 挂起交互卡片 + 结束原因。只消费传入状态，不持有任何自己的状态。
 *
 * memo 是必须的：useChat 的返回值引用只在聊天状态真变时更换，宿主页面
 * 因打字等无关状态的高频重渲染到此为止——否则每个按键都要 reconcile
 * 整条消息流。
 */
export const ChatStream = memo(function ChatStream({
  chat,
}: {
  chat: ChatStreamSource
}) {
  const { t } = useTranslation()
  const flavor = chat.session?.agentFlavor
  const userName = chat.session?.tenantName

  // 出错的那条消息给个重试入口：错误多半来自服务端过载这类与内容无关的
  // 意外，让用户把同一段话手打第二遍是没道理的。轮次在跑时不给——那还
  // 没到「失败」。
  //
  // 判定不能只看 session.state：打开会话会把它拨回 idle（Open 的常规动作），
  // 出错的痕迹留在 stopReason 上——正常收尾是 end_turn，其余（错误原因、
  // cancelled、max_tokens）都意味着这一轮没给出完整回答，都值得给重试。
  const lastStop = chat.stopReason ?? chat.session?.stopReason ?? ""
  const failed =
    !chat.busy &&
    (chat.error !== null ||
      chat.session?.state === "error" ||
      (lastStop !== "" && lastStop !== "end_turn"))
  const retryableId = useMemo(() => {
    if (!failed) return undefined
    for (let i = chat.messages.length - 1; i >= 0; i--) {
      if (chat.messages[i].role === "user") return chat.messages[i].id
    }
    return undefined
  }, [failed, chat.messages])

  // 打开落底稳定器。消息条挂着 content-visibility:auto，屏外内容按
  // 估算高度占位，滚动原语一次性 scrollTop=scrollHeight 会停在半路
  // （滚动过程中真实渲染不断改写 scrollHeight）。初次内容就绪后连续
  // 几帧把视口钉到底直到高度收敛；用户一碰滚轮立刻让位。
  const viewportRef = useRef<HTMLDivElement>(null)
  const pinnedOnce = useRef(false)
  const hasMessages = chat.messages.length > 0
  useEffect(() => {
    if (pinnedOnce.current || !hasMessages) return
    const el = viewportRef.current
    if (!el) return
    pinnedOnce.current = true
    let cancelled = false
    let tries = 0
    let stableFrames = 0
    const stop = () => {
      cancelled = true
    }
    const settle = () => {
      if (cancelled) return
      const target = el.scrollHeight - el.clientHeight
      if (Math.abs(el.scrollTop - target) > 4) {
        el.scrollTop = target
        stableFrames = 0
      } else {
        stableFrames++
      }
      // 连续几帧稳在底部才算收敛（content-visibility 的真实渲染会分几帧
      // 改写 scrollHeight）；一秒兜底，不无限纠缠。
      if (stableFrames < 5 && ++tries < 60) requestAnimationFrame(settle)
    }
    el.addEventListener("wheel", stop, { passive: true })
    el.addEventListener("touchstart", stop, { passive: true })
    requestAnimationFrame(settle)
    return () => {
      cancelled = true
      el.removeEventListener("wheel", stop)
      el.removeEventListener("touchstart", stop)
    }
  }, [hasMessages])

  // 子代理干活的工具调用不进主流（去子代理面板），这里先摘干净再分类。
  // 四趟遍历合成一次 memo：正文分片每 80ms 换一次状态，而工具清单在两次
  // 工具事件之间是不动的，没道理跟着分片重算。
  const { liveEdits, liveOthers, activeTool } = useMemo(() => {
    const main = chat.liveTools.filter((tool) => !tool.subagentOf)
    return {
      // 文件编辑独立成消息条，其余工具调用照旧进「思考与工具调用」折叠区。
      liveEdits: main.filter((tool) => tool.kind === "edit"),
      liveOthers: main.filter((tool) => tool.kind !== "edit"),
      // 折叠头上显示「正在干的那件事」：最后一个未完成的工具调用。
      activeTool: main.findLast(
        (tool) => tool.status !== "completed" && tool.status !== "failed"
      ),
    }
  }, [chat.liveTools])
  const liveActivityCount =
    (chat.streamingThought ? 1 : 0) +
    liveOthers.length +
    chat.permissions.length
  // 一轮开始就在等 agent 说第一句话，这时候没有任何内容可挂——单独一条
  // 「思考中」占位。
  const idleBusy =
    chat.busy &&
    !chat.streamingText &&
    liveActivityCount === 0 &&
    !chat.elicitation &&
    !chat.permission

  // 分组只依赖消息列表：流式分片、工具状态等高频更新不重算。
  const blocks = useMemo(() => groupMessages(chat.messages), [chat.messages])
  // 头像只戳每轮的第一条 agent 内容，其余留空槽。
  const { starts: turnStarts, liveStartsTurn } = useMemo(
    () => turnStartsOf(blocks),
    [blocks]
  )
  // 活内容（这一轮还在跑的部分）按渲染顺序排一遍，头像给排在最前的那块。
  // 声明式地先算好，别在 JSX 里边渲染边翻标志位——那是渲染期改渲染期的
  // 状态，React 编译器会拦。
  const liveOrder = [
    chat.plan && chat.plan.length > 0 ? "plan" : null,
    liveActivityCount > 0 ? "activity" : null,
    liveEdits.length > 0 ? "edits" : null,
    chat.streamingText ? "text" : null,
    chat.permission ? "permission" : null,
    chat.elicitation ? "elicitation" : null,
    idleBusy ? "busy" : null,
  ]
  const liveHead = liveStartsTurn ? liveOrder.find(Boolean) : null
  const liveAvatar = (slot: string) =>
    slot === liveHead ? (
      <AgentAvatar flavor={flavor} name={chat.session?.agentName} />
    ) : undefined

  return (
    // 打开即定位到底部看最新内容（不是先渲染顶部再跳）；
    // autoScroll 让流式输出贴底跟随（用户上翻自动暂停、滚回底部恢复），
    // 「加载更早」prepend 时保持视口位置不跳。
    <MessageScrollerProvider defaultScrollPosition="end" autoScroll>
      {/* @container 是给索引条用的：它按**面板**宽度决定收不收，而不是
          按视口——对话面板在工作区里可以被拖成一条窄缝。 */}
      <MessageScroller className="@container">
        {/* 提问索引贴在左侧空白列，不进滚动区（跟着滚就不是索引了）。 */}
        <MessageIndex
          sessionId={chat.session?.id ?? 0}
          busy={chat.busy}
          loadEarlier={chat.loadEarlier}
        />
        <MessageScrollerViewport ref={viewportRef} preserveScrollOnPrepend>
          <MessageScrollerContent className="mx-auto w-full max-w-3xl px-4 pt-4 pb-48 lg:px-6">
            {/* 历史区单独 memo：轮内一动不动，不该跟着流式每帧重建
                （见 chat-history.tsx）。 */}
            <ChatHistory
              blocks={blocks}
              turnStarts={turnStarts}
              flavor={flavor}
              agentName={chat.session?.agentName}
              userName={userName}
              hasEarlier={chat.hasEarlier}
              loadEarlier={chat.loadEarlier}
              retryableId={retryableId}
              onRetry={chat.retry}
            />

            {/* 任务计划：随 plan 事件实时更新；轮结束后由重建的 plan
                快照消息（历史卡）接力展示最终状态。 */}
            {chat.plan && chat.plan.length > 0 ? (
              <MessageScrollerItem scrollAnchor={false}>
                <AgentRow avatar={liveAvatar("plan")}>
                  <PlanCard entries={chat.plan} />
                </AgentRow>
              </MessageScrollerItem>
            ) : null}

            {liveActivityCount > 0 ? (
              <MessageScrollerItem scrollAnchor={false}>
                <AgentRow avatar={liveAvatar("activity")}>
                  <ActivitySection
                    count={liveActivityCount}
                    busy={chat.busy}
                    activeLabel={
                      activeTool
                        ? activeTool.title || activeTool.kind
                        : undefined
                    }
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
                    {liveOthers.map((tool) => (
                      <LiveToolMarker key={tool.id} tool={tool} />
                    ))}
                  </ActivitySection>
                </AgentRow>
              </MessageScrollerItem>
            ) : null}

            {/* 正在触碰的文件：locations 的跟随指示，点击可在查看器打开。 */}
            {chat.busy && chat.touched[0] ? (
              <MessageScrollerItem scrollAnchor={false}>
                <AgentRow>
                  <TouchedFile loc={chat.touched[0]} cwd={chat.session?.cwd} />
                </AgentRow>
              </MessageScrollerItem>
            ) : null}

            {/* 进行中的文件编辑：独立消息条实时更新，diff 随改随看。 */}
            {liveEdits.map((tool) => (
              <MessageScrollerItem key={tool.id} scrollAnchor={false}>
                <AgentRow avatar={liveAvatar("edits")}>
                  <FileEditCard
                    payload={
                      {
                        kind: tool.kind,
                        rawInput: tool.rawInput,
                        rawOutput: tool.rawOutput,
                        content: tool.content,
                      } as ToolCallPayload
                    }
                    status={tool.status}
                  />
                </AgentRow>
              </MessageScrollerItem>
            ))}

            {chat.streamingText ? (
              <MessageScrollerItem scrollAnchor={false}>
                <AgentRow avatar={liveAvatar("text")}>
                  <StreamingMarkdown>{chat.streamingText}</StreamingMarkdown>
                </AgentRow>
              </MessageScrollerItem>
            ) : null}

            {chat.permission ? (
              <MessageScrollerItem
                key={chat.permission.id}
                scrollAnchor={false}
              >
                <AgentRow avatar={liveAvatar("permission")}>
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
                </AgentRow>
              </MessageScrollerItem>
            ) : null}

            {chat.elicitation ? (
              <MessageScrollerItem
                key={chat.elicitation.id}
                scrollAnchor={false}
              >
                <AgentRow avatar={liveAvatar("elicitation")}>
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
                </AgentRow>
              </MessageScrollerItem>
            ) : null}

            {idleBusy ? (
              <MessageScrollerItem scrollAnchor={false}>
                <AgentRow avatar={liveAvatar("busy")}>
                  <Marker role="status">
                    <MarkerIcon>
                      <Spinner />
                    </MarkerIcon>
                    <MarkerContent className="text-shimmer">
                      {t("chat.thinking")}
                    </MarkerContent>
                  </Marker>
                </AgentRow>
              </MessageScrollerItem>
            ) : null}

            {/* 非正常结束原因内联在消息流末尾，紧跟被截断的回答。 */}
            {chat.stopReason ? (
              <MessageScrollerItem scrollAnchor={false}>
                <AgentRow>
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
                </AgentRow>
              </MessageScrollerItem>
            ) : null}
          </MessageScrollerContent>
        </MessageScrollerViewport>
        <MessageScrollerButton className="data-[direction=end]:bottom-40" />
      </MessageScroller>
    </MessageScrollerProvider>
  )
})
