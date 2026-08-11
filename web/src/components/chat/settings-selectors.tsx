import { useTranslation } from "react-i18next"

import type {
  AccessLevel,
  EffortLevel,
  SessionSettings,
  SettingsPatch,
} from "@/types/acp"
import { cn } from "@/lib/utils"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  BrainIcon,
  ListTodoIcon,
  ShieldIcon,
  SparklesIcon,
  ZapIcon,
} from "lucide-react"

/**
 * 统一设置选择器：模型 / 思考深度 / 权限档三个下拉 + plan / fast 两个开关。
 * 只认后端的统一 Settings 视图，不出现任何 runtime 特有的字面量；
 * 空数组 / 不支持的维度直接不渲染对应控件。
 */
export function SettingsSelectors({
  settings,
  disabled,
  onApply,
}: {
  settings: SessionSettings | null
  disabled: boolean
  onApply: (patch: SettingsPatch) => Promise<void>
}) {
  const { t } = useTranslation()
  if (!settings) return null

  return (
    <>
      {settings.models.length > 0 ? (
        <CapSelect
          icon={<SparklesIcon className="size-3.5" />}
          value={settings.currentModel ?? ""}
          placeholder={t("chat.settings.model")}
          // 只显示模型名：带描述的双行条目会把长清单撑到遮挡。
          options={settings.models.map((m) => ({
            value: m.id,
            name: m.name,
          }))}
          disabled={disabled}
          onChange={(v) => void onApply({ model: v })}
        />
      ) : null}

      {settings.efforts.length > 0 ? (
        <CapSelect
          icon={<BrainIcon className="size-3.5" />}
          value={settings.currentEffort ?? ""}
          placeholder={t("chat.settings.effortLabel")}
          options={settings.efforts.map((e) => ({
            value: e,
            name: t(`chat.settings.effort.${e}`),
          }))}
          disabled={disabled}
          onChange={(v) => void onApply({ effort: v as EffortLevel })}
        />
      ) : null}

      {settings.levels.length > 0 ? (
        <CapSelect
          icon={<ShieldIcon className="size-3.5" />}
          value={settings.currentLevel ?? ""}
          placeholder={t("chat.settings.levelLabel")}
          options={settings.levels.map((l) => ({
            value: l,
            name: t(`chat.settings.level.${l}`),
            description: t(`chat.settings.levelDesc.${l}`),
          }))}
          disabled={disabled}
          onChange={(v) => void onApply({ level: v as AccessLevel })}
        />
      ) : null}

      {settings.planSupported ? (
        <SettingToggle
          icon={<ListTodoIcon className="size-3.5" />}
          label={t("chat.settings.plan")}
          on={settings.planOn}
          disabled={disabled}
          onToggle={(on) => void onApply({ plan: on })}
        />
      ) : null}

      {settings.fastSupported ? (
        <SettingToggle
          icon={<ZapIcon className="size-3.5" />}
          label={t("chat.settings.fast")}
          on={settings.fastOn}
          disabled={disabled}
          onToggle={(on) => void onApply({ fast: on })}
        />
      ) : null}
    </>
  )
}

/** 胶囊式开关：开启时主色浸染，与选择器同排使用。 */
function SettingToggle({
  icon,
  label,
  on,
  disabled,
  onToggle,
}: {
  icon: React.ReactNode
  label: string
  on: boolean
  disabled: boolean
  onToggle: (on: boolean) => void
}) {
  return (
    <button
      type="button"
      aria-pressed={on}
      disabled={disabled}
      onClick={() => onToggle(!on)}
      className={cn(
        "flex h-7 items-center gap-1 rounded-full px-2.5 text-xs transition-[scale,background-color,color] duration-150 ease-snappy active:scale-[0.97] disabled:pointer-events-none disabled:opacity-50",
        on
          ? "bg-primary/15 text-primary"
          : "text-muted-foreground hover:bg-muted hover:text-foreground"
      )}
    >
      {icon}
      {label}
    </button>
  )
}

function CapSelect({
  icon,
  value,
  placeholder,
  options,
  disabled,
  onChange,
}: {
  icon?: React.ReactNode
  value: string
  /** 当前值不在选项里（如 runtime 默认态）时显示的占位文案。 */
  placeholder?: string
  options: { value: string; name: string; description?: string }[]
  disabled: boolean
  onChange: (value: string) => void
}) {
  const current = options.find((o) => o.value === value)

  return (
    <Select
      value={value}
      onValueChange={(v) => {
        if (typeof v === "string" && v && v !== value) onChange(v)
      }}
    >
      <SelectTrigger
        size="sm"
        disabled={disabled}
        className="h-7 gap-1 rounded-full border-transparent bg-transparent px-2.5 text-xs text-muted-foreground shadow-none transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97] dark:bg-transparent dark:hover:bg-muted"
      >
        {icon}
        <SelectValue>{current?.name ?? (value || placeholder)}</SelectValue>
      </SelectTrigger>
      {/* 输入框贴底，向上弹出才不被视口截断；关掉选中项对齐的覆盖式定位，
          并放宽默认的可用高度限制，短清单一屏放完不内滚。 */}
      <SelectContent
        side="top"
        alignItemWithTrigger={false}
        className="max-h-96"
      >
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            <div className="flex min-w-0 flex-col">
              <span>{o.name}</span>
              {o.description ? (
                <span className="max-w-64 truncate text-xs text-muted-foreground">
                  {o.description}
                </span>
              ) : null}
            </div>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
