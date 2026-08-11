import { useTranslation } from "react-i18next"

import type { useDraftSession } from "@/hooks/use-draft-session"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { BotIcon, SparklesIcon } from "lucide-react"

/**
 * 草稿态的模型选择：跨 ACP 分组清单（选模型即选 agent）。
 * 会话创建后被 SettingsSelectors 取代——那时模型只能在当前 ACP 内切。
 * 工作目录不在这里：它与老会话共用输入卡下沿的 ComposerStatus。
 */
export function DraftControls({
  draft,
}: {
  draft: ReturnType<typeof useDraftSession>
}) {
  const { t } = useTranslation()

  const { agents, groups, choiceKey, setChoiceKey, selected, selectedAgent } =
    draft

  return (
    <>
      {agents !== null && agents.length > 0 ? (
        <Select
          value={choiceKey}
          onValueChange={(v) => {
            if (typeof v === "string" && v) setChoiceKey(v)
          }}
        >
          <SelectTrigger
            size="sm"
            aria-label={t("sessions.form.modelLabel")}
            disabled={draft.creating}
            className="h-7 gap-1 rounded-full border-transparent bg-transparent px-2.5 text-xs text-muted-foreground shadow-none transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97] dark:bg-transparent dark:hover:bg-muted"
          >
            <SparklesIcon className="size-3.5" />
            <SelectValue>
              {selected
                ? groups.length > 1
                  ? `${selectedAgent?.name} · ${selected.label}`
                  : selected.label
                : t("sessions.form.modelPlaceholder")}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {groups.map(({ agent, choices }) => (
              <SelectGroup key={agent.id}>
                <SelectLabel className="flex items-center gap-1.5 text-xs uppercase">
                  <BotIcon className="size-3" />
                  {agent.name}
                </SelectLabel>
                {choices.map((choice) => (
                  <SelectItem key={choice.key} value={choice.key}>
                    <div className="flex min-w-0 flex-col">
                      <span>{choice.label}</span>
                      {choice.description ? (
                        <span className="max-w-64 truncate text-xs text-muted-foreground">
                          {choice.description}
                        </span>
                      ) : null}
                    </div>
                  </SelectItem>
                ))}
              </SelectGroup>
            ))}
          </SelectContent>
        </Select>
      ) : null}
    </>
  )
}
