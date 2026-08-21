/**
 * 查看器的类型判定：这个文件该交给谁画。
 *
 * 单独成文件是被 react-refresh 逼的（组件文件只许导出组件），但也确实
 * 该分开——「按扩展名归类」是纯决策，与怎么渲染无关。
 */

/** 浏览器能原生画出来的种类；null 表示只能当文本/二进制处理。 */
export type PreviewKind = "image" | "video" | "audio" | "pdf"

const KIND_BY_EXT: Record<string, PreviewKind> = {
  png: "image",
  jpg: "image",
  jpeg: "image",
  gif: "image",
  webp: "image",
  avif: "image",
  bmp: "image",
  ico: "image",
  svg: "image",
  mp4: "video",
  webm: "video",
  mov: "video",
  mp3: "audio",
  wav: "audio",
  ogg: "audio",
  m4a: "audio",
  flac: "audio",
  pdf: "pdf",
}

/** 只按扩展名判断——与服务端的 inline 白名单是同一套约定。 */
export function previewKind(path: string | null): PreviewKind | null {
  if (!path) return null
  const ext = path.split(".").pop()?.toLowerCase()
  return ext ? (KIND_BY_EXT[ext] ?? null) : null
}
