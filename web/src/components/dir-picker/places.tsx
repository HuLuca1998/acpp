import { useTranslation } from "react-i18next"
import type { TFunction } from "i18next"

import type { FsPlace } from "@/types/acp"
import { cn } from "@/lib/utils"
import {
  DownloadIcon,
  FileTextIcon,
  FolderIcon,
  HomeIcon,
  MonitorIcon,
  BriefcaseIcon,
  XIcon,
  type LucideIcon,
} from "lucide-react"

const PLACE_ICONS: Record<string, LucideIcon> = {
  home: HomeIcon,
  desktop: MonitorIcon,
  documents: FileTextIcon,
  downloads: DownloadIcon,
  workspace: BriefcaseIcon,
}

/** 用户固定的目录（localStorage，随浏览器走）。 */
export interface PinnedDir {
  name: string
  path: string
}

// key 来自后端枚举；i18n 类型增强不吃模板 key，逐个映射。
function placeLabel(t: TFunction, key: string): string {
  switch (key) {
    case "home":
      return t("dirPicker.placeHome")
    case "desktop":
      return t("dirPicker.placeDesktop")
    case "documents":
      return t("dirPicker.placeDocuments")
    case "downloads":
      return t("dirPicker.placeDownloads")
    case "workspace":
      return t("dirPicker.placeWorkspace")
    default:
      return key
  }
}

function PlaceRow({
  icon: Icon,
  label,
  active,
  onClick,
  onRemove,
  removeLabel,
}: {
  icon: LucideIcon
  label: string
  active: boolean
  onClick: () => void
  onRemove?: () => void
  removeLabel?: string
}) {
  return (
    <div className="group relative flex items-center">
      <button
        type="button"
        className={cn(
          "flex w-full min-w-0 items-center gap-1.5 rounded-md px-2 py-1 text-left text-sm transition-colors hover:bg-muted focus-visible:bg-muted focus-visible:outline-none",
          active && "bg-muted"
        )}
        onClick={onClick}
      >
        <Icon className="size-4 shrink-0 text-muted-foreground" />
        <span className="truncate">{label}</span>
      </button>
      {onRemove ? (
        // 常显的低调移除钮（不玩 hover 才现身那套，桌面壳里 hover 不可靠）。
        <button
          type="button"
          aria-label={removeLabel}
          title={removeLabel}
          className="absolute right-1 rounded p-0.5 text-muted-foreground/50 transition-colors hover:bg-accent hover:text-foreground"
          onClick={onRemove}
        >
          <XIcon className="size-3" />
        </button>
      ) : null}
    </div>
  )
}

/**
 * 侧边栏：访达收藏夹的骨架。上半是后端给的默认位置（租户只有自己的
 * root，翻不出去），下半是用户固定的目录。
 */
export function DirPlaces({
  places,
  pins,
  currentPath,
  onNavigate,
  onUnpin,
}: {
  places: FsPlace[]
  pins: PinnedDir[]
  currentPath?: string
  onNavigate: (path: string) => void
  onUnpin: (path: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="flex w-40 shrink-0 flex-col gap-0.5 overflow-y-auto border-r border-border pr-2">
      <p className="px-2 pb-1 text-xs text-muted-foreground">
        {t("dirPicker.places")}
      </p>
      {places.map((place) => (
        <PlaceRow
          key={place.path}
          icon={PLACE_ICONS[place.key] ?? FolderIcon}
          label={placeLabel(t, place.key)}
          active={currentPath === place.path}
          onClick={() => onNavigate(place.path)}
        />
      ))}
      {pins.length > 0 ? (
        <p className="px-2 pt-2 pb-1 text-xs text-muted-foreground">
          {t("dirPicker.pinned")}
        </p>
      ) : null}
      {pins.map((pin) => (
        <PlaceRow
          key={pin.path}
          icon={FolderIcon}
          label={pin.name}
          active={currentPath === pin.path}
          onClick={() => onNavigate(pin.path)}
          onRemove={() => onUnpin(pin.path)}
          removeLabel={t("dirPicker.unpin")}
        />
      ))}
    </div>
  )
}
