import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

import { MASCOT_BUSY, type MascotState } from "@/lib/chat/mascot-state"
import { cn } from "@/lib/utils"
import GrokBall, {
  type GrokBallCreateOptions,
  type GrokBallEngine,
} from "@/vendor/grok-ball"

/** 一轮干完后「笑一下」持续多久，之后落回空闲。 */
const DONE_MS = 1400

/** 目光跟随的灵敏度：鼠标离它这么多像素时眼睛偏到底，再远就不跟了。 */
const GAZE_RANGE = 300

/**
 * 我们的表情态 → grok-ball 的表情 id。
 *
 * 上游的第三组（30–41）本来就是照「代理工作状态」设计的，所以这张表
 * 几乎是一对一，不需要拼凑。id 对照见
 * [vendor/grok-ball/README.md](../../../vendor/grok-ball/README.md) 指向的上游 SKILL.md。
 */
const EMOTION: Record<MascotState, string> = {
  idle: "02", // 待机放空
  typing: "16", // 专注
  thinking: "30", // 思考中
  reading: "40", // 检索资料
  working: "32", // 处理中忙碌
  replying: "39", // 输出回复
  waiting: "35", // 等待输入
  done: "33", // 任务完成
  error: "34", // 出错
  cancelled: "41", // 停止终止
  offline: "00", // 睡眠
}

/** 读屏文案：i18n 的 key 要字面量才能过类型增强，这里摊开写。 */
const LABEL = {
  idle: "chat.mascot.idle",
  typing: "chat.mascot.typing",
  thinking: "chat.mascot.thinking",
  reading: "chat.mascot.reading",
  working: "chat.mascot.working",
  replying: "chat.mascot.replying",
  waiting: "chat.mascot.waiting",
  done: "chat.mascot.done",
  error: "chat.mascot.error",
  cancelled: "chat.mascot.cancelled",
  offline: "chat.mascot.offline",
} as const

/** 引擎只认 6 位 hex（要拿它做球面渐变的 hex 运算），不认 CSS 变量。 */
const HEX = /^#[0-9a-f]{6}$/i

/**
 * 球体与眼睛的颜色从语义 token 现取（定义在 index.css）。
 * 取不到或不是 hex 就整个不传，让引擎用它自己的默认色——宁可形象退回上游
 * 默认，也不要因为一个笔误把球渲染成一团黑。
 */
function readColors(): Pick<GrokBallCreateOptions, "color" | "eyeColor"> {
  const style = getComputedStyle(document.body)
  const body = style.getPropertyValue("--mascot").trim()
  const eye = style.getPropertyValue("--mascot-eye").trim()
  return {
    ...(HEX.test(body) ? { color: body as `#${string}` } : {}),
    ...(HEX.test(eye) ? { eyeColor: eye as `#${string}` } : {}),
  }
}

/**
 * 一轮自然跑完时插进一个瞬时的「任务完成」表情。
 *
 * 刻意不做成 ChatState 的一个态：turn_done 之后聊天状态就是干干净净的
 * 空闲，「刚干完」是吉祥物自己的记忆，没必要污染状态机。
 */
function useDoneFlash(state: MascotState): MascotState {
  const prev = useRef(state)
  const [flash, setFlash] = useState(false)

  useEffect(() => {
    const wasBusy = MASCOT_BUSY.has(prev.current)
    prev.current = state
    // 只有「跑着 → 空闲」才算干完；中止和出错都不是。
    if (!wasBusy || state !== "idle") return
    setFlash(true)
    const timer = setTimeout(() => setFlash(false), DONE_MS)
    return () => clearTimeout(timer)
  }, [state])

  return flash && state === "idle" ? "done" : state
}

/**
 * 输入卡的吉祥物：grok-ball 的白球，蹲在输入卡顶缘上，表情跟着 AI 的工作
 * 状态走，眼睛跟着鼠标看。
 *
 * 渲染引擎是第三方（`vendor/grok-ball`，MIT），我们这一层只负责三件事：
 * 把状态翻译成表情 id、把语义 token 的颜色喂进去、按可见性掐住它的时钟。
 *
 * 落位（蹲在输入卡哪个位置）不在这里，归 composer.tsx——形象与落位分开，
 * 换位置不用碰这个文件。
 */
export function Mascot({
  state,
  className,
}: {
  state: MascotState
  className?: string
}) {
  const { t } = useTranslation()
  const host = useRef<HTMLDivElement>(null)
  const ball = useRef<GrokBallEngine | null>(null)
  // 主题一换球色就得重取：引擎是挂载时把色值烧进 SVG 的，不跟 CSS 变量联动。
  const [themeSeq, setThemeSeq] = useState(0)

  const shown = useDoneFlash(state)

  // 挂载引擎。themeSeq 变化时整只重建，这是上游唯一的换色手段。
  useEffect(() => {
    const el = host.current
    if (!el) return
    const engine = GrokBall.create(el, {
      emotion: EMOTION[shown],
      // idle 关掉：待机/睡眠由我们的状态机决定，不让它自己发呆去。
      idle: false,
      ...readColors(),
    })
    ball.current = engine
    return () => {
      ball.current = null
      engine.destroy()
    }
    // shown 只用来定初始表情，后续切换走下面那个 effect，不重建。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [themeSeq])

  // 明暗与 palette 都落在根元素的 class / data-palette 上，盯这两样就够。
  useEffect(() => {
    const observer = new MutationObserver(() => setThemeSeq((n) => n + 1))
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class", "data-palette"],
    })
    return () => observer.disconnect()
  }, [])

  useEffect(() => {
    ball.current?.setEmotion(EMOTION[shown])
  }, [shown])

  /**
   * 掐时钟：引擎是共享 rAF 驱动的，看不见的时候必须停（前端性能三原则的
   * 「看不见零消耗」）。忙的时候一定转；空闲时也保留（眨眼与目光是它唯一
   * 的生命感），但页面一进后台就整只停下。
   */
  const busy = MASCOT_BUSY.has(shown)
  useEffect(() => {
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches
    function sync() {
      ball.current?.setActive(!document.hidden && (busy || !reduce))
    }
    sync()
    document.addEventListener("visibilitychange", sync)
    return () => document.removeEventListener("visibilitychange", sync)
  }, [busy, themeSeq])

  /**
   * 目光跟随鼠标。mousemove 只记坐标、不读布局，测量与写入合到一帧里做，
   * 指针停住就没有任何工作在跑。reduced-motion 下整个监听都不装。
   */
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return

    let raf = 0
    let px = 0
    let py = 0

    function flush() {
      raf = 0
      const el = host.current
      if (!el || !ball.current) return
      // getBoundingClientRect 放在帧里读：每帧最多一次，且只在指针动时。
      const box = el.getBoundingClientRect()
      if (box.width === 0) return
      const dx = (px - (box.left + box.width / 2)) / GAZE_RANGE
      const dy = (py - (box.top + box.height / 2)) / GAZE_RANGE
      ball.current.setGaze(
        Math.max(-1, Math.min(1, dx)),
        Math.max(-1, Math.min(1, dy))
      )
    }

    function onMove(e: MouseEvent) {
      px = e.clientX
      py = e.clientY
      if (raf === 0) raf = requestAnimationFrame(flush)
    }

    window.addEventListener("mousemove", onMove, { passive: true })
    return () => {
      window.removeEventListener("mousemove", onMove)
      if (raf !== 0) cancelAnimationFrame(raf)
    }
  }, [])

  return (
    <div
      ref={host}
      role="img"
      aria-label={t(LABEL[shown])}
      className={cn("size-full", className)}
    />
  )
}
