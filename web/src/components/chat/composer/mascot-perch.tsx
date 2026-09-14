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
 * 吉祥物在输入卡上的落位与周边交互：蹲顶缘、气泡挨着它往里说、右键给一个
 * 小菜单（安静 / 换边 / 关掉）。
 *
 * 单独成一块而不是写在 composer 里：输入卡本身的职责是输入，吉祥物是挂在
 * 它边上的一整套东西（定位、气泡、偏好、手势），混在一起两边都读不清。
 *
 * 整条是 pointer-events-none，只有球自己可点——它浮在输入卡上方，吃掉指针
 * 事件就会挡住上面的消息流。
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
  /** 输入卡上方被别的东西占了（补全菜单、排队条），先退场别叠在一起。 */
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
    <div
      aria-hidden={ducked}
      className={cn(
        "pointer-events-none absolute inset-x-0 top-0 flex h-14 items-center gap-2 px-5",
        "-translate-y-[55%] transition-[opacity,translate] duration-150 ease-snappy",
        // 左侧落位：DOM 顺序仍是「气泡在前」，靠 row-reverse 把球翻到外侧。
        prefs.side === "left" ? "flex-row-reverse justify-end" : "justify-end",
        ducked && "-translate-y-[20%] opacity-0 motion-reduce:translate-y-0"
      )}
    >
      <MascotBubble
        speech={prefs.quiet || ducked ? null : speech}
        side={prefs.side}
        reveal={(hover || showNow) && !ducked}
        override={prefs.quiet || ducked ? null : quip}
      />
      <ContextMenu>
        <ContextMenuTrigger className="pointer-events-auto size-14 shrink-0">
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
