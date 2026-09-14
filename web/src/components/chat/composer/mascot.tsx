import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

import type { MascotState } from "@/lib/chat/mascot-state"
import { cn } from "@/lib/utils"
import GrokBall, {
  type GrokBallCreateOptions,
  type GrokBallEngine,
} from "@/vendor/grok-ball"

/** 目光跟随的灵敏度：鼠标离它这么多像素时眼睛偏到底，再远就不跟了。 */
const GAZE_RANGE = 300

/** 连点判定：这么久之内的连续点击算一串。 */
const COMBO_MS = 1600

/** 连点到第几下开始不高兴 / 开始表情巡演（彩蛋）。 */
const COMBO_ANNOYED = 5
const COMBO_TOUR = 8

/** 巡演每个表情停多久。 */
const TOUR_MS = 900

/** 干完这么多 token 才值得撒花；小活安静收工就行。 */
const CONFETTI_TOKENS = 5000

/**
 * 我们的表情态 → grok-ball 的表情 id。
 *
 * 上游的第三组（30–41）本来就是照「代理工作状态」设计的，所以工作态几乎
 * 是一对一；生命周期那组（00–07）刚好补上「没人理它」的几个态。
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
  dozing: "04", // 发呆
  asleep: "00", // 睡眠
  waking: "07", // 抖动唤醒
  error: "34", // 出错
  cancelled: "41", // 停止终止
  offline: "06", // 休眠（与 asleep 的「睡着」区分：这是连不上）
}

/** 连点烦了时摆的脸，不进 MascotState——它不是状态，是被逗出来的反应。 */
const EMOTION_ANNOYED = "21" // 生气

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
  dozing: "chat.mascot.dozing",
  asleep: "chat.mascot.asleep",
  waking: "chat.mascot.waking",
  error: "chat.mascot.error",
  cancelled: "chat.mascot.cancelled",
  offline: "chat.mascot.offline",
} as const

/**
 * 某些状态下目光有去处，就别再跟着鼠标跑——**看向该看的地方本身就是信息**：
 * 能发送时它盯着发送键催你，等你裁决时它往上看那张卡。
 * 值是 [x, y]，右下为正，与 setGaze 一致。
 */
const LOOK: Partial<Record<MascotState, [number, number]>> = {
  waiting: [-0.15, -0.85], // 抬头看上面那张权限卡
}

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
 * 输入卡的吉祥物：grok-ball 的白球，表情跟着 AI 的工作状态走，眼睛跟着鼠标看，
 * 点它有反应，没人理就自己发呆睡觉。
 *
 * 渲染引擎是第三方（`vendor/grok-ball`，MIT），我们这一层只负责四件事：
 * 把状态翻译成表情 id、把语义 token 的颜色喂进去、按可见性掐住它的时钟、
 * 接住用户的手势。
 *
 * **它永远不挂有副作用的动作**——中止、发送、删除一概不给。一个会自己动、
 * 还可能被误触的角色去承载真实操作，迟早出事；它只负责表达和逗你，
 * 逻辑全在正经控件上。
 *
 * 落位（蹲输入卡哪边、多大）不在这里，归 composer.tsx。
 */
export function Mascot({
  state,
  quiet = false,
  celebrateTokens,
  onPoke,
  onHoverChange,
  className,
}: {
  state: MascotState
  /** 安静模式：保留表情与目光，不转圈、不撒花。 */
  quiet?: boolean
  /** 本轮 token 量；大活干完才撒花，小活安静收工（`done` 时读）。 */
  celebrateTokens?: number
  /** 被点了一下。调用方据此把气泡顶出来。 */
  onPoke?: (combo: number) => void
  /** 鼠标进出它身上——调用方据此让气泡即时冒出来。 */
  onHoverChange?: (over: boolean) => void
  className?: string
}) {
  const { t } = useTranslation()
  const host = useRef<HTMLDivElement>(null)
  const ball = useRef<GrokBallEngine | null>(null)
  // 主题一换球色就得重取：引擎是挂载时把色值烧进 SVG 的，不跟 CSS 变量联动。
  const [themeSeq, setThemeSeq] = useState(0)

  // 挂载引擎。主题变化时整只重建，这是上游唯一的换色手段。
  useEffect(() => {
    const el = host.current
    if (!el) return
    const engine = GrokBall.create(el, {
      emotion: EMOTION[state],
      // idle 关掉：发呆与睡觉由我们的 useMascotFace 决定，不让它自己跑一套。
      idle: false,
      ...readColors(),
    })
    ball.current = engine
    return () => {
      ball.current = null
      engine.destroy()
    }
    // state 只用来定初始表情，后续切换走下面那个 effect，不重建。
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

  // 表情跟着状态走；顺带给几个瞬间加上身体反应。
  useEffect(() => {
    const engine = ball.current
    if (!engine) return
    if (engine.touring) engine.stopTour()
    engine.setEmotion(EMOTION[state])
    if (quiet) return
    // 出错时弹一下：被自己搞出来的错惊到。
    if (state === "error") engine.bounce()
    // 干完撒花只给大活——每轮都撒花，撒花就不再是奖励了。
    if (state === "done" && (celebrateTokens ?? 0) >= CONFETTI_TOKENS) {
      engine.burst()
    }
  }, [state, quiet, celebrateTokens])

  /** 掐时钟：引擎是共享 rAF 驱动的，看不见的时候必须停。 */
  useEffect(() => {
    function sync() {
      ball.current?.setActive(!document.hidden)
    }
    sync()
    document.addEventListener("visibilitychange", sync)
    return () => document.removeEventListener("visibilitychange", sync)
  }, [])

  /**
   * 目光。有固定去处时看那儿，否则跟着鼠标。
   * mousemove 只记坐标、不读布局，测量与写入合到一帧里做，指针停住就没有
   * 任何工作在跑；reduced-motion 下整个监听都不装。
   */
  const look = LOOK[state] ?? null
  useEffect(() => {
    if (look) {
      ball.current?.setGaze(look[0], look[1])
      return
    }
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
  }, [look])

  /**
   * 手势。全部只作用在它自己身上，一个都不会碰会话。
   * 单击转一圈并把气泡顶出来；连点会把它逗烦；再点下去是表情巡演彩蛋。
   */
  const combo = useRef({ n: 0, at: 0 })
  function poke() {
    const engine = ball.current
    const now = Date.now()
    const n = now - combo.current.at < COMBO_MS ? combo.current.n + 1 : 1
    combo.current = { n, at: now }
    onPoke?.(n)
    if (!engine || quiet) return
    if (n >= COMBO_TOUR) {
      // 彩蛋：把 32 个表情连着放一遍，再点一下停。
      if (engine.touring) engine.stopTour()
      else
        engine.startTour(
          GrokBall.EMOTIONS.map((e) => e.id),
          TOUR_MS
        )
      return
    }
    if (n >= COMBO_ANNOYED) {
      engine.setEmotion(EMOTION_ANNOYED)
      engine.bounce()
      return
    }
    engine.spin()
  }

  return (
    <button
      type="button"
      aria-label={t(LABEL[state])}
      className={cn(
        // 不给 cursor-pointer：桌面 app 用箭头光标（规范 §5.0）。
        "pointer-events-auto size-full shrink-0 appearance-none bg-transparent p-0",
        className
      )}
      onClick={poke}
      onDoubleClick={() => {
        if (!quiet) ball.current?.burst()
      }}
      onMouseEnter={() => onHoverChange?.(true)}
      onMouseLeave={() => onHoverChange?.(false)}
    >
      {/* 刻意不配 Hint：它一动就会自己冒气泡说话，再加一个 tooltip 就是两个
          气泡打架（规范 §5.2 允许「查过判断不合适」，理由留在这儿）。 */}
      <div ref={host} className="size-full" />
    </button>
  )
}
