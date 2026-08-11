import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { ArrowUpIcon, SquareIcon } from "lucide-react"

/**
 * 聊天输入卡：吸底渐变 + 毛玻璃材质，左下角放上下文控件（children），
 * 右下角按状态切换 发送 / 中止 / 创建中。会话页与草稿页共用。
 */
export function Composer({
  value,
  onChange,
  onSubmit,
  onCancel,
  busy = false,
  pending = false,
  disabled = false,
  placeholder,
  children,
  footer,
}: {
  value: string
  onChange: (value: string) => void
  onSubmit: () => void
  /** 一轮进行中时的中止动作；busy 且提供了它才显示中止键。 */
  onCancel?: () => void
  /** 一轮正在跑：显示中止键，Esc 触发中止。 */
  busy?: boolean
  /** 正在创建/发送首条消息：发送键变 spinner。 */
  pending?: boolean
  disabled?: boolean
  placeholder: string
  children?: React.ReactNode
  /** 输入卡下沿的状态栏（工作目录 / 分支 / 用量等）。 */
  footer?: React.ReactNode
}) {
  const { t } = useTranslation()
  const canSend = !disabled && !pending && value.trim() !== ""

  return (
    <div className="pointer-events-none absolute inset-x-0 bottom-0 z-10">
      <div className="absolute inset-x-0 -top-8 bottom-0 bg-gradient-to-t from-background via-background/70 to-transparent" />
      <div className="relative mx-auto w-full max-w-3xl px-4 pb-4 lg:px-6">
        {/* 材质卡片：半透明 + 背景模糊 + 顶缘受光的亮边，读作一块浮起的玻璃。 */}
        <div className="pointer-events-auto rounded-2xl border border-border/60 bg-card/80 shadow-lg shadow-black/5 backdrop-blur-xl dark:border-border dark:[box-shadow:inset_0_1px_0_rgba(255,255,255,0.06),0_8px_32px_rgba(0,0,0,0.35)]">
          <textarea
            value={value}
            rows={1}
            placeholder={placeholder}
            disabled={disabled}
            className="max-h-48 w-full resize-none overflow-y-auto bg-transparent px-4 pt-3.5 pb-1 text-sm outline-none [field-sizing:content] placeholder:text-muted-foreground disabled:opacity-50"
            onChange={(e) => onChange(e.target.value)}
            onKeyDown={(e) => {
              if (
                e.key === "Enter" &&
                !e.shiftKey &&
                !e.nativeEvent.isComposing
              ) {
                e.preventDefault()
                if (canSend && !busy) onSubmit()
              }
              // Esc 中止当前轮：等价于点中止按钮，手不用离开键盘。
              if (e.key === "Escape" && busy && onCancel) {
                e.preventDefault()
                onCancel()
              }
            }}
          />
          <div className="flex flex-wrap items-center gap-0.5 px-2 pb-2">
            {children}
            {busy && onCancel ? (
              <Button
                size="icon-sm"
                variant="outline"
                className="ml-auto rounded-full"
                aria-label={t("chat.stop")}
                onClick={onCancel}
              >
                <SquareIcon className="size-3.5" />
              </Button>
            ) : (
              <Button
                size="icon-sm"
                className="ml-auto rounded-full"
                aria-label={t("chat.send")}
                disabled={!canSend}
                onClick={onSubmit}
              >
                {pending ? (
                  <Spinner className="size-4" />
                ) : (
                  <ArrowUpIcon className="size-4" />
                )}
              </Button>
            )}
          </div>
        </div>
        {footer ? (
          <div className="pointer-events-auto px-2 pt-1.5">{footer}</div>
        ) : null}
      </div>
    </div>
  )
}
