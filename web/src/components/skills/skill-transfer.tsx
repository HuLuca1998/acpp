import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { DownloadIcon, UploadIcon } from "lucide-react"

/**
 * 技能库的换设备搬家：整库导出成一个 zip，在新机器上导入回来。
 *
 * 导出走浏览器原生下载（`<a download>`），不经 fetch 转 blob——带 cookie、
 * 不占内存，进度与另存为都是系统的。导入回来的技能一律停用，由用户在页面
 * 上确认内容后再打开，所以导入后只提示结果并让调用方刷新列表。
 */
export function SkillTransfer({ onImported }: { onImported: () => void }) {
  const { t } = useTranslation()
  const picker = useRef<HTMLInputElement>(null)
  const [importing, setImporting] = useState(false)

  async function handleFile(file: File) {
    setImporting(true)
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
    } finally {
      setImporting(false)
    }
  }

  return (
    <>
      <Button
        size="sm"
        variant="outline"
        render={<a href={api.skills.exportUrl()} download />}
      >
        <DownloadIcon data-icon="inline-start" />
        {t("skills.exportAll")}
      </Button>
      <Button
        size="sm"
        variant="outline"
        disabled={importing}
        onClick={() => picker.current?.click()}
      >
        {importing ? (
          <Spinner data-icon="inline-start" />
        ) : (
          <UploadIcon data-icon="inline-start" />
        )}
        {t("skills.import")}
      </Button>
      <input
        ref={picker}
        type="file"
        accept=".zip,application/zip"
        className="hidden"
        onChange={(e) => {
          const file = e.target.files?.[0]
          // 清掉选中值：同一个文件再导入一次也要能触发 change。
          e.target.value = ""
          if (file) void handleFile(file)
        }}
      />
    </>
  )
}
