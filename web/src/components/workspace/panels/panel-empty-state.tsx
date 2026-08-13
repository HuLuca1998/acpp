import { FolderTreeIcon } from "lucide-react"

import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"

/** 面板内空态：树/预览/diff/logs 各面板共用的轻量壳。 */
export function PanelEmptyState({
  title,
  description,
  action,
}: {
  title: string
  description?: string
  action?: React.ReactNode
}) {
  return (
    <Empty className="h-full justify-center">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <FolderTreeIcon />
        </EmptyMedia>
        <EmptyTitle className="text-sm">{title}</EmptyTitle>
        {description ? (
          <EmptyDescription className="text-xs">{description}</EmptyDescription>
        ) : null}
      </EmptyHeader>
      {action}
    </Empty>
  )
}
