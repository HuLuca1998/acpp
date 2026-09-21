import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { api } from "@/lib/api"
import { TransferButtons } from "@/components/transfer-buttons"

/**
 * 连接配置的换设备搬家：服务器与数据源一起导出成一份 jsonl，在新机器上
 * 导入回来。服务器页与数据库页共用这一对按钮——两张表本来就是一件事的
 * 两面（数据源的跳板机就在服务器表里），分开搬会在新机器上接不回去。
 *
 * 导出**不含凭证**，所以导入后要提醒补密码，否则人会以为搬完就能连。
 */
export function ConnectionTransfer({ onImported }: { onImported: () => void }) {
  const { t } = useTranslation()

  async function handle(file: File) {
    try {
      const res = await api.connections.importJsonl(file)
      onImported()
      if (res.imported.length === 0) {
        toast.warning(t("connections.importNothing"), {
          description: res.skipped
            .map(
              (s) => `${s.name} — ${t(`connections.importSkip.${s.reason}`)}`
            )
            .join("\n"),
        })
        return
      }
      const notes: string[] = [
        t("connections.needSecret", { count: res.needSecret.length }),
      ]
      if (res.skipped.length > 0) {
        notes.push(
          t("connections.importSkipped", { count: res.skipped.length })
        )
      }
      toast.success(t("connections.imported", { count: res.imported.length }), {
        description: notes.join(" "),
      })
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  return (
    <>
      <TransferButtons
        exportUrl={api.connections.exportUrl()}
        exportLabel={t("connections.export")}
        importLabel={t("connections.import")}
        accept=".jsonl,application/x-ndjson,application/json"
        onPick={handle}
      />
      {/* 导出的是明文凭证，按钮旁边就得说清楚——文件名也标了 -secrets。 */}
      <span className="text-xs text-muted-foreground">
        {t("connections.exportHint")}
      </span>
    </>
  )
}
