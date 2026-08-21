import { useTranslation } from "react-i18next"

import type { PreviewKind } from "@/components/workspace/panels/file-preview-kind"

/**
 * 查看器里的「浏览器自己会画」的那部分：图片、音视频、PDF。
 *
 * 这些格式没必要各写一个渲染器——服务端用 `inline=1` 给出真实类型（并
 * 打上沙箱头），浏览器画得比我们好。这里只负责选对标签、摆正位置。
 */

export function MediaPreview({
  kind,
  src,
  name,
}: {
  kind: PreviewKind
  /** 服务端的 inline 直链（见 api 的 previewUrl）。 */
  src: string
  name: string
}) {
  const { t } = useTranslation()

  if (kind === "image") {
    return (
      <div className="flex min-h-full items-center justify-center p-4">
        {/* 棋盘垫底：透明 PNG 落在纸面色上会看不出边界，也分不清哪块是
            图哪块是背景。 */}
        <img
          src={src}
          alt={name}
          className="max-w-full rounded-md bg-[repeating-conic-gradient(var(--color-muted)_0%_25%,transparent_0%_50%)] bg-[length:16px_16px] object-contain shadow-sm"
        />
      </div>
    )
  }

  if (kind === "video") {
    return (
      <div className="flex min-h-full items-center justify-center p-4">
        <video src={src} controls className="max-h-full max-w-full rounded-md">
          {t("workspace.preview.mediaUnsupported")}
        </video>
      </div>
    )
  }

  if (kind === "audio") {
    return (
      <div className="flex min-h-full items-center justify-center p-4">
        <audio src={src} controls className="w-full max-w-md">
          {t("workspace.preview.mediaUnsupported")}
        </audio>
      </div>
    )
  }

  return (
    // 服务端已经给这个响应打了 CSP sandbox，这里的 sandbox 属性是第二道
    // ——嵌进来的 PDF 不该有任何脚本能力。
    <iframe
      src={src}
      title={name}
      sandbox=""
      className="size-full border-0 bg-muted"
    />
  )
}
