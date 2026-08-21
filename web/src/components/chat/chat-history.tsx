import { memo } from "react"

import {
  ActivityMessage,
  ActivitySection,
  ChatMessage,
  EarlierSentinel,
} from "@/components/chat/chat-messages"
import { FileEditCard } from "@/components/chat/file-edit-card"
import { AgentAvatar, AgentRow } from "@/components/chat/message-shell"
import type { ToolCallPayload } from "@/components/chat/tool-call"
import { MessageScrollerItem } from "@/components/ui/message-scroller"
import { blockKey, type Block } from "@/lib/chat/message-blocks"
import type { AgentFlavor } from "@/types/acp"

/**
 * 已成为历史的那部分对话。
 *
 * 单独成组件、单独 memo，是因为它与实时区的更新频率差了两个数量级：
 * 流式期间聊天状态每 80ms 换一次（正文分片合帧），而历史在整轮里一动不
 * 动。放在一起的话，每一帧都要为几百条历史重新创建元素、走一遍协调——
 * 消息条本身有 memo 挡着不重渲染，但**元素创建与 diff 的账照付**，长会话
 * 上那就是流式期间挥之不去的底噪。
 *
 * 入参刻意全是原始值与「只随消息变」的引用（blocks / turnStarts 由上层
 * useMemo 算好），memo 才拦得住。新增入参前先确认它在轮内是稳定的。
 */
export const ChatHistory = memo(function ChatHistory({
  blocks,
  turnStarts,
  flavor,
  agentName,
  userName,
  hasEarlier,
  loadEarlier,
}: {
  blocks: Block[]
  /** 哪些块是「一轮的开头」——头像只戳在那儿。 */
  turnStarts: Set<string>
  flavor?: AgentFlavor
  agentName?: string
  /** 会话创建者的名字，人这侧的头像用它取首字母。 */
  userName?: string
  hasEarlier: boolean
  /** 拉一页更早的消息；返回这一轮有没有拿到新内容。 */
  loadEarlier: () => Promise<boolean>
}) {
  return (
    <>
      {hasEarlier ? (
        <MessageScrollerItem scrollAnchor={false}>
          <EarlierSentinel onVisible={loadEarlier} />
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
          <AgentAvatar flavor={flavor} name={agentName} />
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
                  payload={(block.message.payload ?? {}) as ToolCallPayload}
                  status={
                    (block.message.payload as ToolCallPayload | null)?.status
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
    </>
  )
})
