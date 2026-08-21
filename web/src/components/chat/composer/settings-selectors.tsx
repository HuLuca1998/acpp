import { useTranslation } from "react-i18next"

import type {
  AccessLevel,
  EffortLevel,
  SessionSettings,
  SettingsPatch,
} from "@/types/acp"
import { cn } from "@/lib/utils"
import { Hint } from "@/components/hint"
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
  busy = false,
  onApply,
}: {
  settings: SessionSettings | null
  /** 控件整体不可用（草稿态正在创建会话）。 */
  disabled: boolean
  /** 一轮正在跑：只有权限档与思考深度还能改，见下面的锁定策略。 */
  busy?: boolean
  onApply: (patch: SettingsPatch) => Promise<void>
}) {
  const { t } = useTranslation()
  if (!settings) return null

  // 轮进行中锁住模型 / 计划 / 快速：换模型是换一整条上下文的事，plan 与
  // fast 改了当前轮也不认——留着能点只会让人以为这一轮变了。权限档与
  // 思考深度反过来：前者是「agent 正在乱来，立刻管住它」的唯一手段，
  // 后者是给下一轮预约，两个都得在轮里够得着。
  const lockedInTurn = disabled || busy
  // 生效时机两端不同，照实说：claude 每次工具调用现读权限档，切了这一轮
  // 剩下的操作立刻照新档走；codex 的档位是轮开始时的快照，本轮不认
  // （2026-08 实测）。认不出的 runtime 按保守的「下一轮」说——说小了不会
  // 骗人，说大了会。
  const levelNote = busy
    ? settings.flavor === "claude"
      ? t("chat.settings.inTurn.levelNow")
      : t("chat.settings.inTurn.levelNext")
    : undefined
  const effortNote = busy ? t("chat.settings.inTurn.effort") : undefined
  const lockedNote = busy ? t("chat.settings.inTurn.locked") : undefined

  return (
    <>
      {settings.models.length > 0 ? (
        <CapSelect
          icon={<SparklesIcon className="size-3.5" />}
          label={t("chat.settings.model")}
          desc={t("chat.settings.hints.model")}
          value={settings.currentModel ?? ""}
          placeholder={t("chat.settings.model")}
          // 只显示模型名：带描述的双行条目会把长清单撑到遮挡。
          options={settings.models.map((m) => ({
            value: m.id,
            name: m.name,
          }))}
          disabled={lockedInTurn}
          note={lockedNote}
          onChange={(v) => void onApply({ model: v })}
        />
      ) : null}

      {settings.efforts.length > 0 ? (
        <CapSelect
          icon={<BrainIcon className="size-3.5" />}
          label={t("chat.settings.effortLabel")}
          desc={t("chat.settings.hints.effort")}
          value={settings.currentEffort ?? ""}
          placeholder={t("chat.settings.effortLabel")}
          options={settings.efforts.map((e) => ({
            value: e,
            name: t(`chat.settings.effort.${e}`),
          }))}
          disabled={disabled}
          note={effortNote}
          onChange={(v) => void onApply({ effort: v as EffortLevel })}
        />
      ) : null}

      {settings.levels.length > 0 ? (
        <CapSelect
          icon={<ShieldIcon className="size-3.5" />}
          label={t("chat.settings.levelLabel")}
          desc={t("chat.settings.hints.level")}
          value={settings.currentLevel ?? ""}
          placeholder={t("chat.settings.levelLabel")}
          options={settings.levels.map((l) => ({
            value: l,
            name: t(`chat.settings.level.${l}`),
            description: t(`chat.settings.levelDesc.${l}`),
          }))}
          disabled={disabled}
          note={levelNote}
          onChange={(v) => void onApply({ level: v as AccessLevel })}
        />
      ) : null}

      {settings.planSupported ? (
        <SettingToggle
          icon={<ListTodoIcon className="size-3.5" />}
          label={t("chat.settings.plan")}
          desc={t("chat.settings.hints.plan")}
          on={settings.planOn}
          disabled={lockedInTurn}
          note={lockedNote}
          onToggle={(on) => void onApply({ plan: on })}
        />
      ) : null}

      {settings.fastSupported ? (
        <SettingToggle
          icon={<ZapIcon className="size-3.5" />}
          label={t("chat.settings.fast")}
          desc={t("chat.settings.hints.fast")}
          on={settings.fastOn}
          disabled={lockedInTurn}
          note={lockedNote}
          onToggle={(on) => void onApply({ fast: on })}
        />
      ) : null}
    </>
  )
}

/** 说明气泡的第二段：基础说明，加一行轮进行中的生效时机（有才显示）。 */
function HintDesc({ desc, note }: { desc: string; note?: string }) {
  if (!note) return <>{desc}</>
  return (
    <>
      {desc}
      <span className="mt-1 block text-foreground">{note}</span>
    </>
  )
}

/** 胶囊式开关：开启时主色浸染，与选择器同排使用。 */
function SettingToggle({
  icon,
  label,
  desc,
  note,
  on,
  disabled,
  onToggle,
}: {
  icon: React.ReactNode
  label: string
  /** 悬停说明：开关名本身说不清它到底改了什么。 */
  desc: string
  /** 轮进行中的补充说明（生效时机 / 为什么点不动）。 */
  note?: string
  on: boolean
  disabled: boolean
  onToggle: (on: boolean) => void
}) {
  return (
    <Hint label={label} desc={<HintDesc desc={desc} note={note} />}>
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
    </Hint>
  )
}

function CapSelect({
  icon,
  label,
  desc,
  note,
  value,
  placeholder,
  options,
  disabled,
  onChange,
}: {
  icon?: React.ReactNode
  /** 维度名（模型 / 思考深度 / 权限档）：胶囊上只显示当前值，
   *  不说它是哪个维度的——那句话由悬停说明补。 */
  label: string
  desc: string
  /** 轮进行中的补充说明（生效时机 / 为什么点不动）。 */
  note?: string
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
      <Hint label={label} desc={<HintDesc desc={desc} note={note} />}>
        <SelectTrigger
          size="sm"
          aria-label={label}
          disabled={disabled}
          className="h-7 gap-1 rounded-full border-transparent bg-transparent px-2.5 text-xs text-muted-foreground shadow-none transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97] dark:bg-transparent dark:hover:bg-muted"
        >
          {icon}
          <SelectValue>{current?.name ?? (value || placeholder)}</SelectValue>
        </SelectTrigger>
      </Hint>
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
