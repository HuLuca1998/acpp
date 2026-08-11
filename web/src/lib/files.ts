import type { ImageAttachment } from "@/types/acp"

/** 把浏览器 File 读成 base64 图片附件（去掉 data: 前缀）。 */
export function fileToImageAttachment(file: File): Promise<ImageAttachment> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      const url = reader.result as string
      const comma = url.indexOf(",")
      resolve({
        data: url.slice(comma + 1),
        mimeType: file.type || "image/png",
      })
    }
    reader.onerror = () => reject(reader.error ?? new Error("read failed"))
    reader.readAsDataURL(file)
  })
}

/** 从粘贴事件里取出图片文件。 */
export function imagesFromClipboard(items: DataTransferItemList): File[] {
  const files: File[] = []
  for (const item of items) {
    if (item.kind === "file" && item.type.startsWith("image/")) {
      const file = item.getAsFile()
      if (file) files.push(file)
    }
  }
  return files
}
