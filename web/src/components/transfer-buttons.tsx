import { useRef, useState } from "react"

import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { DownloadIcon, UploadIcon } from "lucide-react"

/**
 * 「导出 / 导入」这对按钮：技能库与连接配置的换设备搬家共用。
 *
 * 导出走浏览器原生下载（`<a download>`）而不是 fetch 转 blob——带 cookie、
 * 不占内存，进度与另存为都是系统的。导入只负责选文件并把忙碌态画出来，
 * 上传与结果提示留给调用方：两处的结果形状与文案都不一样。
 */
export function TransferButtons({
  exportUrl,
  exportLabel,
  importLabel,
  accept,
  onPick,
}: {
  exportUrl: string
  exportLabel: string
  importLabel: string
  accept: string
  onPick: (file: File) => Promise<void>
}) {
  const picker = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)

  return (
    <>
      <Button
        size="sm"
        variant="outline"
        render={<a href={exportUrl} download />}
      >
        <DownloadIcon data-icon="inline-start" />
        {exportLabel}
      </Button>
      <Button
        size="sm"
        variant="outline"
        disabled={busy}
        onClick={() => picker.current?.click()}
      >
        {busy ? (
          <Spinner data-icon="inline-start" />
        ) : (
          <UploadIcon data-icon="inline-start" />
        )}
        {importLabel}
      </Button>
      <input
        ref={picker}
        type="file"
        accept={accept}
        className="hidden"
        onChange={(e) => {
          const file = e.target.files?.[0]
          // 清掉选中值：同一个文件再导入一次也要能触发 change。
          e.target.value = ""
          if (!file) return
          setBusy(true)
          void onPick(file).finally(() => setBusy(false))
        }}
      />
    </>
  )
}
