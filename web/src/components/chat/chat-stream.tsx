import { useTranslation } from "react-i18next"

import {
  ActivityMessage,
  ActivitySection,
  ChatMessage,
  EarlierSentinel,
  LiveToolMarker,
} from "@/components/chat/chat-messages"
import { ElicitationCard } from "@/components/chat/cards/elicitation-card"
import { FileEditCard } from "@/components/chat/file-edit-card"
import { MarkdownContent } from "@/components/chat/markdown"
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
import { groupMessages, type Block } from "@/lib/chat/message-blocks"
import { BrainIcon, CircleAlertIcon, ShieldCheckIcon } from "lucide-react"

/**
 * ChatStream 消费的数据源：聊天状态 + 三个交互回调。结构接口而不是
 * 绑死 useChat 的返回值——任何返回结构兼容形状的流状态机都能渲染。
 */
export interface ChatStreamSource extends ChatState {
  loadEarlier: () => Promise<void> | void
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
 */
export function ChatStream({ chat }: { chat: ChatStreamSource }) {
  const { t } = useTranslation()
  const flavor = chat.session?.agentFlavor
  const userName = chat.session?.tenantName

  // 子代理干活的工具调用不进主流（去子代理面板），这里先摘干净再分类。
  const mainTools = chat.liveTools.filter((tool) => !tool.subagentOf)
  // 文件编辑独立成消息条，其余工具调用照旧进「思考与工具调用」折叠区。
  const liveEdits = mainTools.filter((tool) => tool.kind === "edit")
  const liveOthers = mainTools.filter((tool) => tool.kind !== "edit")
  const liveActivityCount =
    (chat.streamingThought ? 1 : 0) +
    liveOthers.length +
    chat.permissions.length
  // 折叠头上显示「正在干的那件事」：最后一个未完成的工具调用。
  const activeTool = [...mainTools]
    .reverse()
    .find((tool) => tool.status !== "completed" && tool.status !== "failed")
  // 一轮开始就在等 agent 说第一句话，这时候没有任何内容可挂——单独一条
  // 「思考中」占位。
  const idleBusy =
    chat.busy &&
    !chat.streamingText &&
    liveActivityCount === 0 &&
    !chat.elicitation &&
    !chat.permission

  const blocks = groupMessages(chat.messages)
  // 头像只戳每轮的第一条 agent 内容，其余留空槽。
  const { starts: turnStarts, liveStartsTurn } = turnStartsOf(blocks)
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
      <MessageScroller>
        <MessageScrollerViewport preserveScrollOnPrepend>
          <MessageScrollerContent className="mx-auto w-full max-w-3xl px-4 pt-4 pb-48 lg:px-6">
            {chat.hasEarlier ? (
              <MessageScrollerItem scrollAnchor={false}>
                <EarlierSentinel onVisible={() => void chat.loadEarlier()} />
              </MessageScrollerItem>
            ) : null}
            {blocks.map((block) => {
              const key = blockKey(block)
              // 用户消息自带靠右的头像行，不进 agent 侧的对齐槽。
              // 这里内联判断而不用 isUserBlock：类型谓词会把 else 分支里的
              // chat 块一起排除掉，下面就取不到 block.message 了。
              if (block.type === "chat" && block.message.role === "user") {
                // 不设 scrollAnchor：锚定会把新用户消息滚到视口顶并
                // 打断贴底跟随，与 autoScroll 的跟随体验相互矛盾。
                return (
                  <MessageScrollerItem key={key} messageId={key}>
                    <ChatMessage message={block.message} userName={userName} />
                  </MessageScrollerItem>
                )
              }
              const avatar = turnStarts.has(key) ? (
                <AgentAvatar flavor={flavor} name={chat.session?.agentName} />
              ) : undefined
              return (
                <MessageScrollerItem
                  key={key}
                  messageId={block.type === "activity" ? undefined : key}
                  scrollAnchor={false}
                >
                  <AgentRow avatar={avatar}>
                    {block.type === "chat" ? (
                      <ChatMessage message={block.message} />
                    ) : block.type === "edit" ? (
                      <FileEditCard
                        payload={
                          (block.message.payload ?? {}) as ToolCallPayload
                        }
                        status={
                          (block.message.payload as ToolCallPayload | null)
                            ?.status
                        }
                      />
                    ) : (
                      <ActivitySection count={block.items.length}>
                        {block.items.map((item) => (
                          <ActivityMessage key={item.id} message={item} />
                        ))}
                      </ActivitySection>
                    )}
                  </AgentRow>
                </MessageScrollerItem>
              )
            })}

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
                  <TouchedFile
                    loc={chat.touched[0]}
                    cwd={chat.session?.cwd}
                  />
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
                  <MarkdownContent>{chat.streamingText}</MarkdownContent>
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
}

/** 块的稳定 key：活动块自带 key，其余用消息 id。 */
function blockKey(block: Block): string {
  return block.type === "activity" ? block.key : String(block.message.id)
}

/**
 * 算出哪些块是「一轮的开头」——头像只戳在那儿，同一轮后续的块留空槽。
 * liveStartsTurn 说的是历史里最后一句是人说的：那这一轮的活内容就是开头。
 */
function turnStartsOf(blocks: Block[]): {
  starts: Set<string>
  liveStartsTurn: boolean
} {
  const starts = new Set<string>()
  let live = true
  for (const block of blocks) {
    if (block.type === "chat" && block.message.role === "user") {
      live = true
      continue
    }
    if (live) {
      starts.add(blockKey(block))
      live = false
    }
  }
  return { starts, liveStartsTurn: live }
}
