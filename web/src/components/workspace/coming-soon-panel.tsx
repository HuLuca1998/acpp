import { useTranslation } from "react-i18next"

import {
  PANEL_ICONS,
  type WorkspacePanelId,
} from "@/components/workspace/workspace-panels"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"

/** diff / commits / terminal 落地前的诚实占位：说明这里将来是什么。 */
export function ComingSoonPanel({ id }: { id: WorkspacePanelId }) {
  const { t } = useTranslation()
  const Icon = PANEL_ICONS[id]
  return (
    <Empty className="h-full justify-center">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <Icon />
        </EmptyMedia>
        <EmptyTitle className="text-sm">
          {t(`workspace.panels.${id}` as never)}
        </EmptyTitle>
        <EmptyDescription className="text-xs">
          {t(`workspace.comingSoon.${id}` as never)}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}
