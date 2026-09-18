import { useEffect, useState, useSyncExternalStore } from "react"
import { useTranslation } from "react-i18next"

import type { MascotSpeech, MascotState } from "@/lib/chat/mascot-state"
import {
  getMascotPrefs,
  saveMascotPrefs,
  subscribeMascotPrefs,
} from "@/lib/chat/mascot-prefs"
import { cn } from "@/lib/utils"
import { Mascot } from "@/components/chat/composer/mascot"
import { MascotBubble } from "@/components/chat/composer/mascot-bubble"
import {
  ContextMenu,
  ContextMenuCheckboxItem,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu"

/** 连点到第几下算「被逗烦了」/「开始表演」，与 mascot.tsx 的手势判定一致。 */
const COMBO_ANNOYED = 5
const COMBO_TOUR = 8

/** 闲着被点时回的嘴，随机挑一句。 */
const QUIPS = ["a", "b", "c", "d"] as const

/** 回的嘴挂多久自己消失——否则「别点了」会一直赖在那儿。 */
const QUIP_MS = 4000

/**
 * 吉祥物在输入卡边上的落位与周边交互：站在输入卡左侧或右侧（与卡顶齐平）、
 * 气泡在它头顶往卡的方向说、右键给一个小菜单（安静 / 换边 / 关掉）。
 *
 * 单独成一块而不是写在 composer 里：输入卡本身的职责是输入，吉祥物是挂在
 * 它边上的一整套东西（定位、气泡、偏好、手势），混在一起两边都读不清。
 *
 * 落在卡**外面**而不是压着顶缘：球是可点的，压在卡上就会盖住第一行文字和
 * 右侧滚动条，鼠标选字、拖滚动条都被它吃掉（用户点名）。气泡仍是
 * pointer-events-none——它伸到消息流上方，吃指针会挡住消息。
 */
export function MascotPerch({
  state,
  speech,
  tokens,
  ducked = false,
}: {
  /** 该摆哪张脸（已含输入卡就地判断的「正在打字」）。 */
  state: MascotState
  /** 气泡该说什么。 */
  speech: MascotSpeech | null
  /** 本轮 token 量：大活干完才撒花。 */
  tokens?: number
  /** 输入卡上方被别的东西占了（补全菜单、排队条），气泡先闭嘴别叠上去。 */
  ducked?: boolean
}) {
  const { t } = useTranslation()
  const prefs = useSyncExternalStore(subscribeMascotPrefs, getMascotPrefs)
  const [hover, setHover] = useState(false)
  /** 点它回的那句嘴。null 表示这会儿没在回嘴，气泡照常说状态。 */
  const [quip, setQuip] = useState<string | null>(null)
  /** 点它但它正忙着：不回嘴，直接把实况顶出来。 */
  const [showNow, setShowNow] = useState(false)

  // 回嘴与「顶出来」都会自己过期。写在计时器回调里，不在 effect body 里改 state。
  useEffect(() => {
    if (quip === null && !showNow) return
    const timer = setTimeout(() => {
      setQuip(null)
      setShowNow(false)
    }, QUIP_MS)
    return () => clearTimeout(timer)
  }, [quip, showNow])

  /**
   * 被点了。这是事件回调，state 在这儿改是正路。
   * 忙着就报实况，闲着就回一句嘴，点太多次它会烦，再点下去是表情巡演彩蛋。
   */
  function onPoke(combo: number) {
    if (combo >= COMBO_TOUR) {
      setQuip(t("chat.mascot.tour"))
      return
    }
    if (combo >= COMBO_ANNOYED) {
      setQuip(t("chat.mascot.annoyed"))
      return
    }
    if (speech !== null) {
      setQuip(null)
      setShowNow(true)
      return
    }
    const key = QUIPS[Math.floor(Math.random() * QUIPS.length)]
    setQuip(
      key === "a"
        ? t("chat.mascot.quip.a")
        : key === "b"
          ? t("chat.mascot.quip.b")
          : key === "c"
            ? t("chat.mascot.quip.c")
            : t("chat.mascot.quip.d")
    )
  }

  if (!prefs.enabled) return null

  const other = prefs.side === "right" ? "left" : "right"

  return (
    // DOM 里永远排在输入卡之后（Tab 先到输入框），换到左边靠 order 翻过去。
    <div
      className={cn(
        "relative size-14 shrink-0",
        prefs.side === "left" && "order-first"
      )}
    >
      <MascotBubble
        speech={prefs.quiet || ducked ? null : speech}
        side={prefs.side}
        reveal={(hover || showNow) && !ducked}
        override={prefs.quiet || ducked ? null : quip}
        className={cn(
          // 头顶、贴球的外缘、往卡的方向伸：那片是消息流底部的渐隐区，
          // 本来就没有可点的东西。
          "absolute bottom-[calc(100%+0.375rem)]",
          prefs.side === "right" ? "right-0" : "left-0"
        )}
      />
      <ContextMenu>
        <ContextMenuTrigger className="pointer-events-auto block size-14">
          <Mascot
            state={state}
            quiet={prefs.quiet}
            celebrateTokens={tokens}
            onHoverChange={setHover}
            onPoke={onPoke}
          />
        </ContextMenuTrigger>
        <ContextMenuContent>
          <ContextMenuCheckboxItem
            checked={prefs.quiet}
            onCheckedChange={(quiet) => saveMascotPrefs({ quiet })}
          >
            {t("chat.mascot.prefs.quiet")}
          </ContextMenuCheckboxItem>
          <ContextMenuItem onClick={() => saveMascotPrefs({ side: other })}>
            {t("chat.mascot.prefs.moveTo", {
              side:
                other === "left"
                  ? t("chat.mascot.prefs.sideLeft")
                  : t("chat.mascot.prefs.sideRight"),
            })}
          </ContextMenuItem>
          <ContextMenuSeparator />
          {/* 只是藏起来，随时能在设置里开回来——所以不按危险操作走确认弹窗。 */}
          <ContextMenuItem onClick={() => saveMascotPrefs({ enabled: false })}>
            {t("chat.mascot.prefs.turnOff")}
          </ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
    </div>
  )
}
