import type { ReactNode } from "react"
import { useTranslation } from "react-i18next"

import type { AgentFlavor } from "@/types/acp"
import { capitalize } from "@/lib/format"
import { AgentIcon } from "@/components/agent-icon"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import {
  Message as MessageRow,
  MessageAvatar,
  MessageContent,
} from "@/components/ui/message"

/**
 * 对话行的外壳：头像与 agent 侧的对齐槽。
 *
 * 槽对**所有** agent 侧内容生效——正文、工具折叠区、计划卡、提问卡缩进
 * 同样多，左边缘落在同一条竖线上。只有每轮的第一条戳头像，后面的留空槽，
 * 读起来是「同一个人接着说」而不是每句话都自报家门。
 */

/** agent 头像：按 runtime 显示品牌图标，认不出的回退 Bot。 */
export function AgentAvatar({
  flavor,
  name,
}: {
  flavor?: AgentFlavor
  name?: string
}) {
  return (
    <Avatar size="sm" title={name}>
      <AvatarFallback className="text-foreground">
        <AgentIcon flavor={flavor} className="size-3.5" />
      </AvatarFallback>
    </Avatar>
  )
}

/**
 * 人这侧的头像：名字首字母。
 *
 * 名字取**会话的创建者**而不是当前登录的人——owner 回看租户的会话时，
 * 那些消息不是他发的，戳自己的头像是在说谎。
 */
export function UserAvatar({ name }: { name?: string }) {
  const { t } = useTranslation()
  // 空的 tenantName 就是 owner 自己（他不在租户表里，adr-007）。
  const label = name ? capitalize(name) : t("identity.admin")
  return (
    <Avatar size="sm" title={label}>
      <AvatarFallback>{[...label][0]}</AvatarFallback>
    </Avatar>
  )
}

/** agent 侧的一行：左侧固定留出头像宽度的槽，avatar 缺省时槽留空。 */
export function AgentRow({
  avatar,
  children,
}: {
  avatar?: ReactNode
  children: ReactNode
}) {
  return (
    <MessageRow>
      {/* 空槽不该画出 MessageAvatar 自带的灰底圆。 */}
      <MessageAvatar className="self-start bg-transparent">
        {avatar}
      </MessageAvatar>
      <MessageContent>{children}</MessageContent>
    </MessageRow>
  )
}
