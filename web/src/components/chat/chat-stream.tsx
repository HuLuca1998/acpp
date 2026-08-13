import { useTranslation } from "react-i18next"

import {
  ActivityMessage,
  ActivitySection,
  ChatMessage,
  EarlierSentinel,
  LiveToolMarker,
} from "@/components/chat/chat-messages"
import { ElicitationCard } from "@/components/chat/elicitation-card"
import { FileEditCard } from "@/components/chat/file-edit-card"
import { MarkdownContent } from "@/components/chat/markdown"
import { PermissionCard } from "@/components/chat/permission-card"
import { PlanCard } from "@/components/chat/plan-card"
import { PlanReviewCard } from "@/components/chat/plan-review-card"
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
import type { useChat } from "@/hooks/use-chat"
import { groupMessages } from "@/lib/message-blocks"
import { BrainIcon, CircleAlertIcon, ShieldCheckIcon } from "lucide-react"

/**
 * 消息流：历史消息块 + 计划卡 + 实时活动区（思考/工具/权限）+ 流式正文
 * + 挂起交互卡片 + 结束原因。只消费 useChat 状态，不持有任何自己的状态。
 */
export function ChatStream({ chat }: { chat: ReturnType<typeof useChat> }) {
  const { t } = useTranslation()

  // 文件编辑独立成消息条，其余工具调用照旧进「思考与工具调用」折叠区。
  const liveEdits = chat.liveTools.filter((tool) => tool.kind === "edit")
  const liveOthers = chat.liveTools.filter((tool) => tool.kind !== "edit")
  const liveActivityCount =
    (chat.streamingThought ? 1 : 0) + liveOthers.length + chat.permissions.length

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
            {groupMessages(chat.messages).map((block) =>
              block.type === "chat" ? (
                // 不设 scrollAnchor：锚定会把新用户消息滚到视口顶并
                // 打断贴底跟随，与 autoScroll 的跟随体验相互矛盾。
                <MessageScrollerItem
                  key={block.message.id}
                  messageId={String(block.message.id)}
                >
                  <ChatMessage message={block.message} />
                </MessageScrollerItem>
              ) : block.type === "edit" ? (
                <MessageScrollerItem
                  key={block.message.id}
                  messageId={String(block.message.id)}
                >
                  <FileEditCard
                    payload={(block.message.payload ?? {}) as ToolCallPayload}
                    status={
                      (block.message.payload as ToolCallPayload | null)?.status
                    }
                  />
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
                <ActivitySection count={liveActivityCount} busy={chat.busy}>
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
                          choice: perm.choice || t("chat.permission.cancelled"),
                        })}
                      </MarkerContent>
                    </Marker>
                  ))}
                  {liveOthers.map((tool) => (
                    <LiveToolMarker key={tool.id} tool={tool} />
                  ))}
                </ActivitySection>
              </MessageScrollerItem>
            ) : null}

            {/* 进行中的文件编辑：独立消息条实时更新，diff 随改随看。 */}
            {liveEdits.map((tool) => (
              <MessageScrollerItem key={tool.id} scrollAnchor={false}>
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
              </MessageScrollerItem>
            ))}

            {chat.streamingText ? (
              <MessageScrollerItem scrollAnchor={false}>
                <MarkdownContent>{chat.streamingText}</MarkdownContent>
              </MessageScrollerItem>
            ) : null}

            {chat.permission ? (
              <MessageScrollerItem key={chat.permission.id} scrollAnchor={false}>
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
  )
}
