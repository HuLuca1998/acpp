import { useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

import {
  MASCOT_SPEECH_PERSISTS,
  MASCOT_SPEECH_SETTLES,
  type MascotSpeech,
} from "@/lib/chat/mascot-state"
import { formatTokens } from "@/lib/format"
import { cn } from "@/lib/utils"

/**
 * 「思考 / 干活」这类会来回切的话，冒泡前先沉这么久。
 *
 * 一轮里表情会在 思考 / 查资料 / 干活 之间来回切好几次（实测 10 秒内切 5
 * 次都有），每次都立刻弹一句，气泡就成了闪灯。结论性的话不走这条，见
 * MASCOT_SPEECH_SETTLES。
 */
const SETTLE_MS = 900

/** 说完这么久自己收回去。等人处理的那几种不在此列，见 MASCOT_SPEECH_PERSISTS。 */
const LINGER_MS = 4000

/**
 * 没话说了之后，上一句还挂这么久才收。
 *
 * 「搞定 · 113.7k token」这种收尾话最需要它：庆祝表情只有 1.4 秒，状态一落
 * 回空闲 speech 就成了 null，不留这条尾巴，那句话根本来不及被看见。
 */
const TAIL_MS = 1400

/**
 * 吉祥物的小气泡：它说的话。
 *
 * 内容不是「思考中」这种废话，而是**它此刻在干的具体那件事**——工具标题、
 * 在等你批哪条命令、这轮花了多少 token。工具标题是 agent 给的原文，不翻译
 * 也不改写：那是现场信息，替它组织语言只会把细节磨掉。
 *
 * 显示什么是**算出来的**，不靠 state 镜像 props：`settled` 只在计时器回调里
 * 落地，一旦当前该说的话变了，旧的 settled 自然对不上就不再显示——不需要在
 * effect 里同步清它（项目开了 react-hooks/set-state-in-effect）。
 */
export function MascotBubble({
  speech,
  side = "right",
  reveal = false,
  override = null,
  className,
}: {
  speech: MascotSpeech | null
  /** 球站在输入卡哪边——尖角落在底边靠球那一头，往下指着它。 */
  side?: "left" | "right"
  /** 鼠标正停在球上：不用沉，立刻说。 */
  reveal?: boolean
  /** 宿主塞进来的一句话（点它回的嘴），盖在状态话之上。 */
  override?: string | null
  className?: string
}) {
  const { t } = useTranslation()

  const text = useMemo(() => {
    if (!speech) return null
    switch (speech.kind) {
      case "thinking":
        return t("chat.mascot.say.thinking")
      case "tool":
        return speech.text
      case "waiting":
        return speech.text
          ? t("chat.mascot.say.waitingWhat", { what: speech.text })
          : t("chat.mascot.say.waiting")
      case "asking":
        return t("chat.mascot.say.asking")
      case "done":
        return speech.tokens
          ? t("chat.mascot.say.doneTokens", {
              tokens: formatTokens(speech.tokens),
            })
          : t("chat.mascot.say.done")
      case "error":
        return speech.text || t("chat.mascot.error")
      case "cancelled":
        return t("chat.mascot.say.cancelled")
      case "offline":
        return t("chat.mascot.say.offline")
    }
  }, [speech, t])

  const persist = speech !== null && MASCOT_SPEECH_PERSISTS.has(speech.kind)

  /** 当前挂在气泡里的那句。**只由计时器回调写入**，effect body 里不改 state。 */
  const [pinned, setPinned] = useState<string | null>(null)

  const settles = speech !== null && MASCOT_SPEECH_SETTLES.has(speech.kind)

  useEffect(() => {
    // 没话说了：留一条尾巴再收，别让收尾话一闪就没。
    if (text === null) {
      const tail = setTimeout(() => setPinned(null), TAIL_MS)
      return () => clearTimeout(tail)
    }
    // 会来回切的沉一下，结论性的下一拍就说。
    const timer = setTimeout(() => setPinned(text), settles ? SETTLE_MS : 0)
    return () => clearTimeout(timer)
  }, [text, settles])

  // 非等待类的话说完自己收；等人处理的一直挂着。
  useEffect(() => {
    if (pinned === null || persist) return
    const timer = setTimeout(() => setPinned(null), LINGER_MS)
    return () => clearTimeout(timer)
  }, [pinned, persist])

  // 悬停时直接用当前该说的话（不等），平时用挂着的那句。
  const shown = override ?? (reveal && text !== null ? text : pinned)
  if (shown === null) return null

  return (
    <div
      // 读屏的信息已经在球的 aria-label 上，这儿再念一遍就是重复。
      aria-hidden
      className={cn(
        "relative max-w-56 truncate rounded-xl border border-border/60 bg-popover/95",
        "px-2.5 py-1.5 text-xs text-foreground shadow-md backdrop-blur-md",
        "transition-[opacity,translate] duration-150 ease-snappy",
        "starting:translate-y-1 starting:opacity-0 motion-reduce:starting:translate-y-0",
        // 底边的小尖角往下指着球：球 56px 宽，尖角中心对到球心（外缘往里 28px）。
        "after:absolute after:-bottom-1 after:size-2 after:rotate-45",
        "after:border-r after:border-b after:border-border/60 after:bg-popover/95",
        side === "right" ? "after:right-6" : "after:left-6",
        className
      )}
    >
      {shown}
    </div>
  )
}
