import { useTranslation } from "react-i18next"
import { CheckCircle2Icon, XCircleIcon } from "lucide-react"

/**
 * 「测一次连接」这个动作的状态机。数据源与服务器两处共用——两边测的东西
 * 不同（MySQL / SSH），但状态与展示完全一样。
 */
export type TestState =
  | { status: "idle" }
  | { status: "running" }
  | { status: "ok"; version?: string }
  | { status: "failed"; error: string }

/**
 * 测试连接的结论。成功文案按测的是什么分档——ssh 档只测到 SSH 那一层，
 * 不能说成「MySQL 连接成功」。
 */
export function TestResult({
  state,
  variant = "mysql",
}: {
  state: TestState
  variant?: "mysql" | "ssh"
}) {
  const { t } = useTranslation()
  if (state.status === "ok") {
    return (
      <p className="flex items-center gap-1.5 text-xs text-success">
        <CheckCircle2Icon className="size-3.5 shrink-0" />
        {variant === "ssh"
          ? t("db.sshTestOk", { version: state.version ?? "" })
          : t("db.testOk", { version: state.version ?? "" })}
      </p>
    )
  }
  if (state.status === "failed") {
    return (
      <p className="flex items-start gap-1.5 text-xs text-destructive">
        <XCircleIcon className="mt-0.5 size-3.5 shrink-0" />
        <span className="min-w-0 break-all">
          {t("db.testFailed")}
          {state.error ? `：${state.error}` : null}
        </span>
      </p>
    )
  }
  return null
}
