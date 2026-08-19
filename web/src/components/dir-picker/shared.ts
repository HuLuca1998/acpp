import type { DirEntry } from "@/types/acp"
import {
  FileArchiveIcon,
  FileCode2Icon,
  FileIcon,
  FileImageIcon,
  FileKey2Icon,
  FileTextIcon,
  type LucideIcon,
} from "lucide-react"

export type DirSortKey = "name" | "mtime" | "size"

/** 组内排序：目录与文件各排各的（目录=导航、文件=选中，交互不同不混排）。 */
export function sortEntries(
  entries: DirEntry[],
  key: DirSortKey,
  asc: boolean
): DirEntry[] {
  return [...entries].sort((a, b) => {
    let cmp = 0
    if (key === "mtime") {
      cmp = (a.modTime ?? "").localeCompare(b.modTime ?? "")
    } else if (key === "size") {
      cmp = a.size - b.size
    }
    if (cmp === 0) {
      cmp = a.name.localeCompare(b.name, undefined, { sensitivity: "base" })
    }
    return asc ? cmp : -cmp
  })
}

const CODE_EXT = new Set([
  "ts", "tsx", "js", "jsx", "mjs", "go", "py", "rs", "rb", "sh", "zsh",
  "c", "h", "cc", "cpp", "java", "swift", "kt", "sql", "json", "yaml",
  "yml", "toml", "xml", "html", "css",
])
const IMAGE_EXT = new Set(["png", "jpg", "jpeg", "gif", "svg", "webp", "icns", "ico"])
const ARCHIVE_EXT = new Set(["zip", "tar", "gz", "tgz", "bz2", "xz", "7z", "dmg"])
const TEXT_EXT = new Set(["md", "txt", "log", "rst"])
const KEY_EXT = new Set(["pem", "key", "ppk", "pub"])

/** 按扩展名给个贴近类型的图标（访达式），认不出的用通用文件图标。 */
export function fileIconOf(name: string): LucideIcon {
  const dot = name.lastIndexOf(".")
  const ext = dot > 0 ? name.slice(dot + 1).toLowerCase() : ""
  // ~/.ssh 里的私钥多半没有扩展名，靠命名习惯认（id_ed25519、id_rsa…）。
  if (KEY_EXT.has(ext) || name.startsWith("id_")) return FileKey2Icon
  if (CODE_EXT.has(ext)) return FileCode2Icon
  if (IMAGE_EXT.has(ext)) return FileImageIcon
  if (ARCHIVE_EXT.has(ext)) return FileArchiveIcon
  if (TEXT_EXT.has(ext)) return FileTextIcon
  return FileIcon
}

/** 访达式紧凑修改时间：同年省年份，跨年才带。 */
export function formatMtime(iso: string | undefined, locale: string): string {
  if (!iso) return "—"
  const d = new Date(iso)
  const sameYear = d.getFullYear() === new Date().getFullYear()
  return d.toLocaleString(locale, {
    year: sameYear ? undefined : "numeric",
    month: "short",
    day: "numeric",
    hour: sameYear ? "2-digit" : undefined,
    minute: sameYear ? "2-digit" : undefined,
  })
}
