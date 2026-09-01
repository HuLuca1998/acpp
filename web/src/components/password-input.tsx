import { useState } from "react"
import { useTranslation } from "react-i18next"
import { EyeIcon, EyeOffIcon } from "lucide-react"

import { cn } from "@/lib/utils"
import { Hint } from "@/components/hint"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"

/**
 * 密码输入框 + 「看一眼」。
 *
 * 编辑已存记录时框里是空的（后端不下发密码，留空即「不修改」），这时点
 * 眼睛要能**把已存的密码取回来填进框**——否则「看一眼」只能看到自己刚敲
 * 的字，对「我当初配的是哪个密码」这个真实问题毫无帮助。取回来之后它就是
 * 一个普通的可选中、可复制的值。
 *
 * 取密码走 owner 专属端点，与 URI 导出同一口径。
 */
export function PasswordInput({
  id,
  value,
  placeholder,
  autoComplete = "off",
  className,
  onChange,
  fetchStored,
}: {
  id?: string
  value: string | undefined
  placeholder?: string
  autoComplete?: string
  className?: string
  onChange: (value: string) => void
  /** 框为空时点「看一眼」，用它取回已存的值。不给则只做显示/隐藏。 */
  fetchStored?: () => Promise<string>
}) {
  const { t } = useTranslation()
  const [shown, setShown] = useState(false)
  const [loading, setLoading] = useState(false)

  async function toggle() {
    if (shown) {
      setShown(false)
      return
    }
    // 框里已经有内容（新输入的，或上次取回来的）就直接显示，不必再请求。
    if (!value && fetchStored) {
      setLoading(true)
      try {
        onChange(await fetchStored())
      } catch {
        // 取不到就只切换显示——让用户看到「空的」，而不是弹一个错打断他。
      } finally {
        setLoading(false)
      }
    }
    setShown(true)
  }

  return (
    <div className="relative">
      <Input
        id={id}
        type={shown ? "text" : "password"}
        autoComplete={autoComplete}
        value={value ?? ""}
        placeholder={placeholder}
        className={cn("pr-9", shown && "font-mono", className)}
        onChange={(e) => onChange(e.target.value)}
      />
      <Hint label={shown ? t("common.hide") : t("common.reveal")} align="end">
        <button
          type="button"
          aria-label={shown ? t("common.hide") : t("common.reveal")}
          className="absolute inset-y-0 right-0 flex w-9 items-center justify-center text-muted-foreground transition-colors hover:text-foreground"
          onClick={toggle}
        >
          {loading ? (
            <Spinner className="size-3.5" />
          ) : shown ? (
            <EyeOffIcon className="size-3.5" />
          ) : (
            <EyeIcon className="size-3.5" />
          )}
        </button>
      </Hint>
    </div>
  )
}
