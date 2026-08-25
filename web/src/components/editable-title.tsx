import { useEffect, useRef, useState } from "react"

import { cn } from "@/lib/utils"

/**
 * 就地改标题。
 *
 * 显示态是一段普通文本，进入编辑态后原地换成输入框：Enter 提交、Esc 取消、
 * 失焦按提交处理（改完点别处就是"我改好了"，让它丢弃反而更意外）。
 *
 * `activateOn` 区分两种落位：独立标题单击即可编辑；而侧栏那种整行是链接的
 * 条目必须用双击——单击得留给"打开这条会话"。
 *
 * 提交交给调用方（异步失败时回滚由调用方决定），这里只保证：空白标题不提交，
 * 没改动也不提交，省掉一次无谓的请求。
 */
export function EditableTitle({
  value,
  onSubmit,
  activateOn = "click",
  className,
  inputClassName,
  title,
}: {
  value: string
  onSubmit: (next: string) => void
  activateOn?: "click" | "doubleClick"
  className?: string
  inputClassName?: string
  /** 悬停提示，通常是「双击可改名」这类说明。 */
  title?: string
}) {
  const [editing, setEditing] = useState(false)
  // 草稿只在进入编辑那一刻从 value 取一次。不用 effect 去同步——在 effect 里
  // 同步 setState 会触发级联渲染（lint 也拦），而这里根本不需要：非编辑态
  // 显示的一直是 value 本身。
  const [draft, setDraft] = useState(value)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!editing) return
    const input = inputRef.current
    if (!input) return
    input.focus()
    input.select()
  }, [editing])

  const commit = () => {
    setEditing(false)
    const next = draft.trim()
    if (!next || next === value) {
      setDraft(value)
      return
    }
    onSubmit(next)
  }

  if (editing) {
    return (
      <input
        ref={inputRef}
        value={draft}
        title={title}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault()
            commit()
          } else if (e.key === "Escape") {
            e.preventDefault()
            setDraft(value)
            setEditing(false)
          }
          // 编辑时的按键不该冒泡给外层的快捷键与列表导航。
          e.stopPropagation()
        }}
        // 在链接/按钮内部编辑时，点击不能穿到外层去触发导航。
        onClick={(e) => e.stopPropagation()}
        onDoubleClick={(e) => e.stopPropagation()}
        className={cn(
          "min-w-0 flex-1 rounded-sm bg-transparent outline-none ring-1 ring-ring/50",
          inputClassName ?? className
        )}
      />
    )
  }

  const activate = (e: React.MouseEvent) => {
    // 外层可能是链接：进入编辑就不该同时跳走。
    e.preventDefault()
    e.stopPropagation()
    setDraft(value)
    setEditing(true)
  }

  return (
    <span
      title={title}
      className={cn("min-w-0 truncate", className)}
      onClick={activateOn === "click" ? activate : undefined}
      onDoubleClick={activateOn === "doubleClick" ? activate : undefined}
    >
      {value}
    </span>
  )
}
