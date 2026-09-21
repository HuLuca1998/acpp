import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { api } from "@/lib/api"
import { TransferButtons } from "@/components/transfer-buttons"

/**
 * 技能库的换设备搬家：整库导出成一个 zip，在新机器上导入回来。
 *
 * 导入回来的技能一律停用，由用户在页面上确认内容后再打开，所以导入后只
 * 提示结果并让调用方刷新列表。
 */
export function SkillTransfer({ onImported }: { onImported: () => void }) {
  const { t } = useTranslation()

  async function handle(file: File) {
    try {
      const res = await api.skills.importZip(file)
      onImported()
      if (res.imported.length === 0 && res.skipped.length > 0) {
        // 一条都没进来时，原因比「成功」重要得多。
        toast.warning(t("skills.importNothing"), {
          description: res.skipped
            .map((s) => `${s.name} — ${t(`skills.importSkip.${s.reason}`)}`)
            .join("\n"),
        })
        return
      }
      toast.success(t("skills.imported", { count: res.imported.length }), {
        description:
          res.skipped.length > 0
            ? t("skills.importSkipped", { count: res.skipped.length })
            : t("skills.importDisabled"),
      })
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  return (
    <TransferButtons
      exportUrl={api.skills.exportUrl()}
      exportLabel={t("skills.exportAll")}
      importLabel={t("skills.import")}
      accept=".zip,application/zip"
      onPick={handle}
    />
  )
}
