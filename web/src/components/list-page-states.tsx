import { useTranslation } from "react-i18next"

import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"

/**
 * 列表页三态的统一壳：加载失败 / 骨架 / 空态（带下一步 CTA）。
 * 有数据时页面自行渲染列表——本组件只在三态之一成立时使用。
 */
export function ListPageStates({
  icon,
  error,
  loading,
  emptyTitle,
  emptyHint,
  emptyAction,
}: {
  /** 页面的主题图标，三态共用。 */
  icon: React.ReactNode
  error: string | null
  loading: boolean
  emptyTitle: string
  emptyHint?: string
  emptyAction?: React.ReactNode
}) {
  const { t } = useTranslation()

  if (error) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">{icon}</EmptyMedia>
          <EmptyTitle>{t("common.loadFailed")}</EmptyTitle>
          <EmptyDescription>{error}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  if (loading) {
    return (
      <div className="flex flex-col gap-2">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-full" />
      </div>
    )
  }
  return (
    <Empty>
      <EmptyHeader>
        <EmptyMedia variant="icon">{icon}</EmptyMedia>
        <EmptyTitle>{emptyTitle}</EmptyTitle>
        {emptyHint ? <EmptyDescription>{emptyHint}</EmptyDescription> : null}
      </EmptyHeader>
      {emptyAction ? <EmptyContent>{emptyAction}</EmptyContent> : null}
    </Empty>
  )
}
