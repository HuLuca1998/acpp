import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { cn } from "@/lib/utils"
import { copyText } from "@/lib/clipboard"
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
    // 复制是用户主动点的，失败必须说出来——静默失败会让人以为按钮是坏的。
    if (!(await copyText(text))) {
      toast.error(t("common.copyFailed"))
      return
    }
    setCopied(true)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setCopied(false), 1500)
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
