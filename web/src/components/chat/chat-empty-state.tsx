import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { MessagesSquareIcon } from "lucide-react"

/** 对话空态：欢迎语 + 建议芯片，点击芯片即发送第一句。 */
export function ChatEmptyState({
  disabled,
  onSuggestion,
}: {
  disabled: boolean
  onSuggestion: (text: string) => void
}) {
  const { t } = useTranslation()
  return (
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
              disabled={disabled}
              onClick={() => onSuggestion(t(`chat.suggestions.${key}`))}
            >
              {t(`chat.suggestions.${key}`)}
            </Button>
          ))}
        </div>
      </EmptyContent>
    </Empty>
  )
}
