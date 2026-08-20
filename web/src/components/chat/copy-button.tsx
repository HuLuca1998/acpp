import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"

import { cn } from "@/lib/utils"
import { Hint } from "@/components/hint"
import { Button } from "@/components/ui/button"
import { CheckIcon, CopyIcon } from "lucide-react"

/**
 * 复制按钮：点击写剪贴板，1.5s 内图标变对勾作为反馈。
 * 用在代码块头部与消息操作行。
 */
export function CopyButton({
  text,
  className,
}: {
  text: string
  className?: string
}) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const timer = useRef<number | undefined>(undefined)

  async function copy() {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      window.clearTimeout(timer.current)
      timer.current = window.setTimeout(() => setCopied(false), 1500)
    } catch {
      // 剪贴板被拒绝（权限/非安全上下文）时静默失败，不打断阅读。
    }
  }

  return (
    <Hint label={copied ? t("chat.copied") : t("chat.copy")}>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        aria-label={copied ? t("chat.copied") : t("chat.copy")}
        className={cn("size-6 text-muted-foreground", className)}
        onClick={() => void copy()}
      >
        {copied ? (
          <CheckIcon className="size-3.5 text-success" />
        ) : (
          <CopyIcon className="size-3.5" />
        )}
      </Button>
    </Hint>
  )
}
