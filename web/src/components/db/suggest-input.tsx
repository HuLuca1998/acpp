import { useRef, useState } from "react"

import { cn } from "@/lib/utils"
import { Input } from "@/components/ui/input"

/**
 * 带建议的输入框：可以自由输入，也可以从已有值里挑一个。
 *
 * 不用原生 `<datalist>`：它的弹层由浏览器画，白底方块 + 系统箭头，
 * 在深色界面里像贴了张纸，而且完全没法样式化。菜单样式与 composer
 * 的斜杠补全对齐，同一个应用里的"建议"看起来就该是同一个东西。
 *
 * 建议是提示不是约束——项目名要能填工作区里还没有的仓库（库常常先于
 * 代码存在，本机没 clone 也得能配连接）。
 */
export function SuggestInput({
  id,
  value,
  options,
  placeholder,
  required,
  onChange,
}: {
  id?: string
  value: string
  options: string[]
  placeholder?: string
  required?: boolean
  onChange: (value: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  // 关闭要等一拍：点击建议时 blur 先于 click 触发，立刻卸载菜单会让
  // 这一次点击落空。
  const closeTimer = useRef<number | undefined>(undefined)

  const query = value.trim().toLowerCase()
  const matches = options
    .filter((o) => o.toLowerCase() !== query && o.toLowerCase().includes(query))
    .slice(0, 8)
  const menuOpen = open && matches.length > 0
  const index = Math.min(active, matches.length - 1)

  function pick(option: string) {
    onChange(option)
    setOpen(false)
    setActive(0)
  }

  return (
    <div className="relative">
      <Input
        id={id}
        value={value}
        required={required}
        placeholder={placeholder}
        autoComplete="off"
        role="combobox"
        aria-expanded={menuOpen}
        onChange={(e) => {
          onChange(e.target.value)
          setOpen(true)
          setActive(0)
        }}
        onFocus={() => setOpen(true)}
        onBlur={() => {
          closeTimer.current = window.setTimeout(() => setOpen(false), 120)
        }}
        onKeyDown={(e) => {
          if (!menuOpen || e.nativeEvent.isComposing) return
          if (e.key === "ArrowDown") {
            e.preventDefault()
            setActive((i) => (i + 1) % matches.length)
          } else if (e.key === "ArrowUp") {
            e.preventDefault()
            setActive((i) => (i - 1 + matches.length) % matches.length)
          } else if (e.key === "Enter" || e.key === "Tab") {
            // Enter 选中建议而不是提交表单——菜单开着时用户想要的是这个。
            e.preventDefault()
            pick(matches[index])
          } else if (e.key === "Escape") {
            e.preventDefault()
            setOpen(false)
          }
        }}
      />
      {menuOpen ? (
        <div className="absolute inset-x-0 top-[calc(100%+0.25rem)] z-50 max-h-48 overflow-y-auto rounded-xl border border-border bg-popover p-1 shadow-lg transition-[opacity,translate] duration-150 ease-snappy starting:-translate-y-1 starting:opacity-0 motion-reduce:starting:translate-y-0">
          {matches.map((option, i) => (
            <button
              key={option}
              type="button"
              className={cn(
                "flex w-full rounded-md px-2 py-1.5 text-left font-mono text-sm",
                i === index ? "bg-accent text-accent-foreground" : "hover:bg-muted"
              )}
              onMouseEnter={() => setActive(i)}
              // mousedown 先于 blur，用它才不会被上面的关闭计时器抢跑。
              onMouseDown={(e) => {
                e.preventDefault()
                window.clearTimeout(closeTimer.current)
                pick(option)
              }}
            >
              {option}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  )
}
