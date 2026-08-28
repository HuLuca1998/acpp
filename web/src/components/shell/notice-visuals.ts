import {
  CircleCheckIcon,
  MessageCircleQuestionMarkIcon,
  OctagonXIcon,
  RefreshCwIcon,
  ShieldQuestionMarkIcon,
} from "lucide-react"

/**
 * 每类通知的图标与色调。照 iOS 横幅的图形语言：图标装进一块色底小方块
 * （相当于 app 图标的位置），颜色只落在这一块上，卡片本身永远是中性纸面色。
 */
export const STYLES = {
  permission: {
    Icon: ShieldQuestionMarkIcon,
    tone: "text-warning",
    tile: "bg-warning/15",
  },
  elicitation: {
    Icon: MessageCircleQuestionMarkIcon,
    tone: "text-warning",
    tile: "bg-warning/15",
  },
  turn_end: {
    Icon: CircleCheckIcon,
    tone: "text-success",
    tile: "bg-success/15",
  },
  error: {
    Icon: OctagonXIcon,
    tone: "text-destructive",
    tile: "bg-destructive/15",
  },
  update: { Icon: RefreshCwIcon, tone: "text-warning", tile: "bg-warning/15" },
  // 撤回信号不会进列表，列在这里只是让类型收口。
  permission_done: {
    Icon: CircleCheckIcon,
    tone: "text-muted-foreground",
    tile: "bg-muted",
  },
  elicitation_done: {
    Icon: CircleCheckIcon,
    tone: "text-muted-foreground",
    tile: "bg-muted",
  },
} as const

export const TITLE_KEYS = {
  permission: "notify.permission",
  elicitation: "notify.elicitation",
  turn_end: "notify.turnEnd",
  error: "notify.error",
  update: "backend.updateAvailable",
  permission_done: "notify.turnEnd",
  elicitation_done: "notify.turnEnd",
} as const
