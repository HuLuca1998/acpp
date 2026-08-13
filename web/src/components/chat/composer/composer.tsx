import { useState } from "react"
import { useTranslation } from "react-i18next"

import type { SlashCommand } from "@/types/acp"
import { imagesFromClipboard } from "@/lib/files"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { ArrowUpIcon, SlashIcon, SquareIcon } from "lucide-react"

/** 模糊匹配：query 的字符按顺序在 text 里出现即命中（子序列，不必连续）。 */
function fuzzyMatch(text: string, query: string): boolean {
  let ti = 0
  for (const qc of query) {
    ti = text.indexOf(qc, ti)
    if (ti === -1) return false
    ti++
  }
  return true
}

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
  commands,
  attachments,
  onPasteImages,
  queue,
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
  /** agent 的斜杠命令清单；输入以 "/" 开头时弹补全菜单。 */
  commands?: SlashCommand[]
  /** 待发送附件预览（textarea 上方）。 */
  attachments?: React.ReactNode
  /** 粘贴进来的图片文件。 */
  onPasteImages?: (files: File[]) => void
  /** 排队的插话条（输入卡上方，busy 期间的消息先排队再发）。 */
  queue?: React.ReactNode
}) {
  const { t } = useTranslation()
  const canSend = !disabled && !pending && value.trim() !== ""

  // "/" 补全：只在整条输入是一个未完成的命令词时出现。
  const slashQuery =
    value.startsWith("/") && !/\s/.test(value) ? value.slice(1) : null
  const matches =
    slashQuery !== null && commands && commands.length > 0
      ? commands
          .filter((c) => {
            const name = c.name.toLowerCase()
            const q = slashQuery.toLowerCase()
            // 命令名带 runtime 前缀：codex 给技能加 $（$my-issues），claude 给
            // 插件技能加 <plugin>:（acpp:my-issues）。剥掉前缀后按技能真名匹配。
            let bare = name.startsWith("$") ? name.slice(1) : name
            if (bare.includes(":")) bare = bare.slice(bare.lastIndexOf(":") + 1)
            // 模糊匹配：query 的字符按顺序出现即命中（不必连续、不必开头），
            // 命令多也能快速搜到——/mi 命中 my-issues，/rvb 命中 review-branch。
            return q === "" || fuzzyMatch(bare, q) || fuzzyMatch(name, q)
          })
          .slice(0, 8)
      : []
  const [activeIndex, setActiveIndex] = useState(0)
  const menuOpen = matches.length > 0
  const active = Math.min(activeIndex, matches.length - 1)

  function pickCommand(name: string) {
    onChange(`/${name} `)
    setActiveIndex(0)
  }

  return (
    <div className="pointer-events-none absolute inset-x-0 bottom-0 z-10">
      <div className="absolute inset-x-0 -top-8 bottom-0 bg-gradient-to-t from-background via-background/70 to-transparent" />
      <div className="relative mx-auto w-full max-w-3xl px-4 pb-4 lg:px-6">
        {queue}
        {/* 材质卡片：半透明 + 背景模糊 + 顶缘受光的亮边，读作一块浮起的玻璃。 */}
        <div
          data-slot="composer-shell"
          className="pointer-events-auto relative rounded-2xl border border-border/60 bg-card/80 backdrop-blur-xl dark:border-border"
        >
          {menuOpen ? (
            <div className="absolute inset-x-2 bottom-[calc(100%+0.5rem)] max-h-64 overflow-y-auto rounded-xl border border-border bg-popover p-1 shadow-lg transition-[opacity,translate] duration-150 ease-snappy starting:translate-y-1 starting:opacity-0 motion-reduce:starting:translate-y-0">
              {matches.map((cmd, index) => (
                <button
                  key={cmd.name}
                  type="button"
                  className={cn(
                    "flex w-full items-baseline gap-2 rounded-md px-2 py-1.5 text-left text-sm",
                    index === active
                      ? "bg-accent text-accent-foreground"
                      : "hover:bg-muted"
                  )}
                  onMouseEnter={() => setActiveIndex(index)}
                  onClick={() => pickCommand(cmd.name)}
                >
                  <SlashIcon className="size-3 shrink-0 self-center text-muted-foreground" />
                  <span className="shrink-0 font-mono">{cmd.name}</span>
                  {cmd.description ? (
                    <span className="truncate text-xs text-muted-foreground">
                      {cmd.description}
                    </span>
                  ) : null}
                </button>
              ))}
            </div>
          ) : null}
          {attachments}
          <textarea
            value={value}
            rows={1}
            placeholder={placeholder}
            disabled={disabled}
            className="[field-sizing:content] max-h-48 w-full resize-none overflow-y-auto bg-transparent px-4 pt-3.5 pb-1 text-sm outline-none placeholder:text-muted-foreground disabled:opacity-50"
            onChange={(e) => {
              onChange(e.target.value)
              setActiveIndex(0)
            }}
            onPaste={(e) => {
              if (!onPasteImages) return
              const files = imagesFromClipboard(e.clipboardData.items)
              if (files.length > 0) {
                e.preventDefault()
                onPasteImages(files)
              }
            }}
            onKeyDown={(e) => {
              // 补全菜单打开时接管方向键与确认键。
              if (menuOpen && !e.nativeEvent.isComposing) {
                if (e.key === "ArrowDown") {
                  e.preventDefault()
                  setActiveIndex((active + 1) % matches.length)
                  return
                }
                if (e.key === "ArrowUp") {
                  e.preventDefault()
                  setActiveIndex((active - 1 + matches.length) % matches.length)
                  return
                }
                if (e.key === "Enter" || e.key === "Tab") {
                  e.preventDefault()
                  pickCommand(matches[active].name)
                  return
                }
                if (e.key === "Escape") {
                  e.preventDefault()
                  onChange(value + " ")
                  return
                }
              }
              // busy 时 Enter 也能发：消息会被插进正在跑的轮。
              if (
                e.key === "Enter" &&
                !e.shiftKey &&
                !e.nativeEvent.isComposing
              ) {
                e.preventDefault()
                if (canSend) onSubmit()
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
