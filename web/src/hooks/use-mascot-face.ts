import { useEffect, useRef, useState } from "react"

import {
  MASCOT_BUSY,
  mascotSpeechOf,
  mascotStateOf,
  type MascotInput,
  type MascotSpeech,
  type MascotState,
} from "@/lib/chat/mascot-state"

/** 一轮干完「庆祝」持续多久，之后落回空闲。 */
const DONE_MS = 1400

/** 从睡着被叫醒时「抖一下」持续多久。 */
const WAKE_MS = 900

/**
 * 没人理它多久开始发呆 / 睡着。
 *
 * 深夜另有一套（睡得快得多）：凌晨两点还开着 acpp 的人，多半是把窗口
 * 摊在那儿没在看，让它陪着熬夜挺假的。
 */
const DOZE = { doze: 90_000, sleep: 300_000 }
const DOZE_NIGHT = { doze: 30_000, sleep: 120_000 }
const NIGHT_FROM = 2
const NIGHT_TO = 5

/** 打盹判定的心跳。10 秒一次，粒度够了，开销可以忽略。 */
const TICK_MS = 10_000

export interface MascotFace {
  /** 该摆哪张脸。 */
  state: MascotState
  /** 气泡该说什么，null 表示闭嘴。 */
  speech: MascotSpeech | null
}

function isNight(): boolean {
  const h = new Date().getHours()
  return h >= NIGHT_FROM && h < NIGHT_TO
}

/**
 * 把聊天状态接成吉祥物「此刻的样子」：聊天派生的表情态之上，叠三个与聊天
 * 无关、只跟人有关的瞬时态——干完的庆祝、没人理的发呆/睡着、被叫醒的抖动。
 *
 * 这些刻意不进 ChatState：`turn_done` 之后聊天状态就是干干净净的空闲，
 * 「刚干完」「困了」是吉祥物自己的记忆，不该污染状态机。
 *
 * 活动检测全部走 ref + 一个 10 秒心跳，**不会因为鼠标动一下就触发重渲染**
 * ——这个 hook 挂在对话面板上，那是正文所在的组件树。
 *
 * 传 null 表示草稿态（还没有会话）：只有空闲与发呆那套，不会误报断线。
 */
export function useMascotFace(input: MascotInput | null): MascotFace {
  const base: MascotState = input ? mascotStateOf(input) : "idle"

  const prevBase = useRef(base)
  const [flash, setFlash] = useState<"done" | "waking" | null>(null)
  const [drowsy, setDrowsy] = useState<"dozing" | "asleep" | null>(null)
  // 0 = 还没记过。渲染期不许调 Date.now()（不是纯函数），首次在下面的
  // 监听 effect 里落地。
  const activeAt = useRef(0)

  // 一轮自然跑完 → 庆祝一下。中止和出错都不算干完，不给。
  useEffect(() => {
    const wasBusy = MASCOT_BUSY.has(prevBase.current)
    prevBase.current = base
    if (!wasBusy || base !== "idle") return
    // 两端都由计时器落地：effect body 里不直接改 state
    //（react-hooks/set-state-in-effect）。开场那个 0ms 只差一拍，看不出来。
    const on = setTimeout(() => setFlash("done"))
    const off = setTimeout(() => setFlash(null), DONE_MS)
    return () => {
      clearTimeout(on)
      clearTimeout(off)
    }
  }, [base])

  // 人一活动就记一笔时间戳。只写 ref，不进 state——这几个监听会在滚动、
  // 打字、划鼠标时高频触发，每次都重渲染就等于把正文一起拖下水。
  useEffect(() => {
    activeAt.current = Date.now()
    function mark() {
      activeAt.current = Date.now()
    }
    const opts = { passive: true } as const
    window.addEventListener("mousemove", mark, opts)
    window.addEventListener("keydown", mark, opts)
    window.addEventListener("pointerdown", mark, opts)
    return () => {
      window.removeEventListener("mousemove", mark)
      window.removeEventListener("keydown", mark)
      window.removeEventListener("pointerdown", mark)
    }
  }, [])

  // 打盹只在真正空闲时算。忙着、等人裁决、出错时它没资格困。
  const idleNow = base === "idle" && flash === null
  useEffect(() => {
    // 不空闲时不清 drowsy：下面算 state 时本来就只在空闲态采信它，清它反而
    // 要在 effect body 里改 state。进空闲先用 0ms 计时器立刻复判一次。
    if (!idleNow) return
    function tick() {
      const quiet = Date.now() - activeAt.current
      const limit = isNight() ? DOZE_NIGHT : DOZE
      setDrowsy(
        quiet >= limit.sleep ? "asleep" : quiet >= limit.doze ? "dozing" : null
      )
    }
    const first = setTimeout(tick)
    const timer = setInterval(tick, TICK_MS)
    return () => {
      clearTimeout(first)
      clearInterval(timer)
    }
  }, [idleNow])

  // 睡着时人一动就抖一下醒过来。这个不等心跳——被叫醒要立刻反应，
  // 迟 10 秒的「惊醒」就不是惊醒了。
  const asleep = drowsy === "asleep" && idleNow
  useEffect(() => {
    if (!asleep) return
    function wake() {
      setDrowsy(null)
      setFlash("waking")
      setTimeout(() => setFlash((f) => (f === "waking" ? null : f)), WAKE_MS)
    }
    const opts = { passive: true } as const
    window.addEventListener("mousemove", wake, opts)
    window.addEventListener("keydown", wake, opts)
    window.addEventListener("pointerdown", wake, opts)
    return () => {
      window.removeEventListener("mousemove", wake)
      window.removeEventListener("keydown", wake)
      window.removeEventListener("pointerdown", wake)
    }
  }, [asleep])

  const state: MascotState =
    flash ?? (idleNow && drowsy !== null ? drowsy : base)

  return {
    state,
    speech: input ? mascotSpeechOf(state, input) : null,
  }
}
