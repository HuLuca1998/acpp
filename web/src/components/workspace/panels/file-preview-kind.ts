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

/** csv/tsv/xlsx：能摊平成行列，交给表格渲染器（与服务端 IsTableFile 对齐）。 */
export function isTableFile(path: string | null): boolean {
  if (!path) return false
  const ext = path.split(".").pop()?.toLowerCase()
  return ext === "csv" || ext === "tsv" || ext === "xlsx" || ext === "xlsm"
}

/** html：既能看渲染结果，也能看源码。 */
export function isHtmlFile(path: string | null): boolean {
  if (!path) return false
  const ext = path.split(".").pop()?.toLowerCase()
  return ext === "html" || ext === "htm"
}

export function isMarkdownFile(path: string | null): boolean {
  if (!path) return false
  const ext = path.split(".").pop()?.toLowerCase()
  return ext === "md" || ext === "markdown"
}

/**
 * 这个文件有没有「源码形态」可看。
 *
 * xlsx 是压缩过的二进制，切到源码只会看到一堆乱码——它只有表格一种形态，
 * 切换按钮就不该出现。csv 有：它本来就是文本，逗号在哪儿有时正是要看的。
 */
export function hasSourceView(path: string | null): boolean {
  if (!path) return false
  const ext = path.split(".").pop()?.toLowerCase()
  return ext !== "xlsx" && ext !== "xlsm"
}

/** 有渲染形态的文件（相对于源码形态）：markdown / html / 表格。 */
export function hasRichView(path: string | null): boolean {
  return isMarkdownFile(path) || isHtmlFile(path) || isTableFile(path)
}
