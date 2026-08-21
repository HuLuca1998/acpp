import { useCallback, useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

import type { OutlineEntry, SessionOutline } from "@/types/acp"
import { useAsyncData } from "@/hooks/use-async-data"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import { Hint } from "@/components/hint"
import {
  useMessageScroller,
  useMessageScrollerVisibility,
} from "@/components/ui/message-scroller"

/**
 * 提问索引条：对话左侧的一列刻度，一格一条用户提问，点了跳过去。
 *
 * 索引来自服务端的 outline 端点，覆盖**整条会话**——消息是分页懒加载的，
 * 但服务端本来就持有全量重建结果，索引不必跟着界面的加载进度走。点到还
 * 没加载进来的老提问时，这里替用户把「加载更早」泵到那一条出现为止。
 *
 * 必须挂在 `MessageScrollerProvider` 内：跳转与「当前读到哪儿」都是滚动
 * 原语给的能力（scrollToMessage / currentAnchorId）。
 */

/** 后台还在精简摘要时的重拉间隔：文案会自己变好，过一会儿再看一眼。 */
const PENDING_REFRESH_MS = 20_000

/** 跳到未加载的老提问时，最多往前泵这么多页（一页 30 条）。 */
const MAX_JUMP_PAGES = 40

/** 连续这么多轮没拿到新消息就放弃跳转——「加载更早」自己也有防重入。 */
const MAX_JUMP_MISSES = 3

/**
 * 静息时露出的刻度数。长会话有几十上百轮，全排出来是一条从头到尾的栅栏，
 * 又吵又没法读——安静时只留当前读到的这一段，手伸过去才展开全部。
 */
const WINDOW_SIZE = 9

/** 草稿会话与拉取失败共用的空索引：常量而不是字面量，免得每次换引用。 */
const EMPTY_OUTLINE: SessionOutline = { items: [], pending: 0 }

export function MessageIndex({
  sessionId,
  busy,
  loadEarlier,
}: {
  sessionId: number
  /** 轮次进行中。轮一结束就重拉：刚发的那句提问要进索引。 */
  busy: boolean
  /** 拉一页更早的消息；返回这一轮有没有拿到新内容。 */
  loadEarlier: () => Promise<boolean>
}) {
  const { t } = useTranslation()
  const [jumping, setJumping] = useState<number | null>(null)
  // 鼠标正指着的那一格（索引下标）。梯队以它为中心，不是以阅读位置为
  // 中心——手在哪儿，放大镜就在哪儿。
  const [hovered, setHovered] = useState<number | null>(null)
  // 鼠标进到索引条这一列：展开全部刻度。移开收回当前这一段。
  const [expanded, setExpanded] = useState(false)
  // 刚点过的那一格。跳转落点会按原语的 peek 露出上一条的一角，只看可见
  // 集合会把高亮判给上一轮——用户刚点的那条当然该亮，直到他自己滚开。
  const [pinned, setPinned] = useState<number | null>(null)
  // 手动重拉的节拍器：后台补完摘要后要再看一眼（见下面的定时器）。
  const [tick, setTick] = useState(0)

  const scroller = useMessageScroller()
  const { visibleMessageIds } = useMessageScrollerVisibility()

  // busy 进 deps 就是「轮末重拉」——这一轮的提问要等转录落盘才进得了索引。
  // 顺带在轮开始时也拉一次：那次多半没有新东西，但它只是服务端的一次内存
  // 遍历，比为了省它而在 effect 里比对前后状态划算。
  const { data } = useAsyncData<SessionOutline>(
    () =>
      sessionId
        ? api.sessions.outline(sessionId)
        : Promise.resolve(EMPTY_OUTLINE),
    [sessionId, busy, tick]
  )
  // 拉失败时 data 留在 null，索引整条不显示。它是导航的便利，不是对话
  // 本身——为它弹一条错误只会打扰正在看回答的人。
  const items = data?.items ?? EMPTY_OUTLINE.items
  const pending = data?.pending ?? 0

  // 后台还在跑模型精简：过一会儿再拉一次，把回落的首行换成摘要。
  useEffect(() => {
    if (pending <= 0) return
    const timer = setTimeout(() => setTick((n) => n + 1), PENDING_REFRESH_MS)
    return () => clearTimeout(timer)
  }, [pending])

  /**
   * 当前读到的那一轮：视口里最靠上那条消息之前最近的一条提问。
   *
   * 刻意不用原语的 `currentAnchorId`——它只在 `scrollAnchor="true"` 的条目
   * 之间追踪，而消息流把锚定全关了（锚定会把新消息滚到视口顶，与贴底
   * 跟随冲突，见 ChatStream）。`visibleMessageIds` 不挑锚点，按 DOM 顺序
   * 给出可见的消息 id，头一个就是最靠上的那条。
   *
   * 比对到「之前最近的一条提问」而不是精确相等：视口里多半是 agent 的
   * 回答，精确比对会一格都点不亮——人在读的是某一轮，索引该亮的就是那
   * 一轮的提问。
   */
  const scrolledId = useMemo(() => {
    const top = Number(visibleMessageIds[0])
    if (!Number.isFinite(top)) return null
    let active: number | null = null
    for (const item of items) {
      if (item.messageId > top) break
      active = item.messageId
    }
    return active
  }, [visibleMessageIds, items])
  const activeId = pinned ?? scrolledId

  /**
   * 静息时露出的那一段：以当前读到的位置为中心，两端不足就往回贴边。
   * 还不知道读到哪儿（刚打开、可见集合没算出来）就露最新的一段——打开
   * 会话本来就落在底部。
   */
  const slice = useMemo(() => {
    if (expanded || items.length <= WINDOW_SIZE) {
      return { start: 0, shown: items }
    }
    const center = items.findIndex((item) => item.messageId === activeId)
    const half = Math.floor(WINDOW_SIZE / 2)
    const max = items.length - WINDOW_SIZE
    const start = center < 0 ? max : Math.min(Math.max(center - half, 0), max)
    return { start, shown: items.slice(start, start + WINDOW_SIZE) }
  }, [expanded, items, activeId])

  // 用户一碰滚轮或键盘就把高亮交还给自动判定。
  useEffect(() => {
    if (pinned === null) return
    const release = () => setPinned(null)
    window.addEventListener("wheel", release, { passive: true, once: true })
    window.addEventListener("keydown", release, { once: true })
    return () => {
      window.removeEventListener("wheel", release)
      window.removeEventListener("keydown", release)
    }
  }, [pinned])

  const jumpTo = useCallback(
    async (messageId: number) => {
      const key = String(messageId)
      // 已经在 DOM 里就是一次普通滚动。刻意**不**用 smooth：索引跳转是
      // 导航不是微调，长会话里一次跳几万像素，平滑滚动要么滚上好几秒、
      // 要么把整条对话糊成一片，到站还比瞬移晚。
      if (scroller.scrollToMessage(key, { align: "start", behavior: "auto" })) {
        setPinned(messageId)
        return
      }

      // 不在 DOM 里 = 这条还没被「加载更早」拉进来，替用户泵到它出现。
      setJumping(messageId)
      try {
        let misses = 0
        for (let i = 0; i < MAX_JUMP_PAGES; i++) {
          const progressed = await loadEarlier()
          // 等 React 提交完这一页再量：DOM 里没有节点，scrollToMessage
          // 就找不到锚点。
          await nextFrame()
          if (scroller.scrollToMessage(key, { align: "start" })) {
            setPinned(messageId)
            return
          }
          // 没进展未必是到头了——顶部哨兵可能正好占着「加载更早」的防重
          // 入锁。连着几轮都没动静才认定拉不到。
          if (progressed) {
            misses = 0
          } else if (++misses >= MAX_JUMP_MISSES) {
            break
          }
        }
      } finally {
        setJumping(null)
      }
    },
    [loadEarlier, scroller]
  )

  // 只有一条提问的会话没什么可索引的，一格刻度反而像界面出了故障。
  if (items.length < 2) return null

  return (
    <div
      // 贴在滚动区左缘的空白列里：正文是 max-w-3xl 居中的，这条列本来就
      // 空着。容器窄到正文要占满时整条收起——压着字读比没有索引更糟。
      className="group/outline pointer-events-none absolute inset-y-0 left-0 z-10 hidden w-10 flex-col justify-center @3xl:flex"
      aria-label={t("chat.outline.title")}
    >
      <div
        className={cn(
          "pointer-events-auto flex max-h-full scrollbar-thin flex-col items-start overflow-y-auto py-2 pl-3"
        )}
        onMouseEnter={() => setExpanded(true)}
        onMouseLeave={() => {
          setExpanded(false)
          setHovered(null)
        }}
      >
        {slice.shown.map((item, i) => (
          <IndexTick
            key={item.messageId}
            entry={item}
            active={item.messageId === activeId}
            // 离鼠标几格：没在指就统一收成短划，指到谁谁最长、邻居次之。
            distance={
              hovered === null ? Infinity : Math.abs(slice.start + i - hovered)
            }
            jumping={item.messageId === jumping}
            onHover={() => setHovered(slice.start + i)}
            onJump={() => void jumpTo(item.messageId)}
          />
        ))}
      </div>
    </div>
  )
}

/**
 * 一格刻度：一条左对齐的短横线。
 *
 * 长度归鼠标管——指着的那条最长、邻居次之，像 Dock 的放大镜，手移到哪儿
 * 哪儿展开；颜色归阅读位置管——当前读的那一轮上亮色。两件事分给两种视觉
 * 通道，同时看得见，互不打架。
 */
function IndexTick({
  entry,
  active,
  distance,
  jumping,
  onHover,
  onJump,
}: {
  entry: OutlineEntry
  /** 当前正在读的那一轮。 */
  active: boolean
  /** 离鼠标指着的那格几格；Infinity 表示鼠标不在索引条上。 */
  distance: number
  jumping: boolean
  onHover: () => void
  onJump: () => void
}) {
  return (
    <Hint
      label={<span className="line-clamp-1 font-medium">{entry.text}</span>}
      desc={
        entry.reply ? (
          <span className="line-clamp-2">{entry.reply}</span>
        ) : undefined
      }
      side="right"
      align="center"
    >
      <button
        type="button"
        aria-label={entry.text}
        aria-current={active ? "true" : undefined}
        onMouseEnter={onHover}
        onFocus={onHover}
        onClick={onJump}
        // 按钮之间不留缝：行高 10px 自己就是间距，留缝只会让鼠标沿着索引
        // 条滑动时掉进空隙、放大镜一闪一闪。
        className="group/tick flex h-2.5 w-6 shrink-0 items-center outline-none"
      >
        <span
          className={cn(
            // 极细极淡的一划：常显是为了随时点得到（藏起来的控件在
            // WKWebView 里根本点不着），但它不该跟正文争注意力。
            "h-px w-6 origin-left rounded-full bg-muted-foreground/50 transition-[background-color,transform] duration-150 ease-snappy motion-reduce:transition-none",
            // 放大镜：指着的那条整根伸出来，邻居依次收回去。差距要够大，
            // 不然一列长短相近的划子反而更乱。
            distance === 0
              ? "scale-x-100"
              : distance === 1
                ? "scale-x-60"
                : distance === 2
                  ? "scale-x-45"
                  : "scale-x-35",
            // 当前读的那一轮压一层亮色，与鼠标焦点的纯白区分开。
            active && "bg-foreground/60",
            distance === 0 && "bg-foreground",
            jumping && "animate-pulse bg-foreground motion-reduce:animate-none"
          )}
        />
      </button>
    </Hint>
  )
}

/** 等一帧让 React 把这一页消息提交进 DOM（双 rAF 兜住并发渲染的提交时机）。 */
function nextFrame(): Promise<void> {
  return new Promise((resolve) =>
    requestAnimationFrame(() => requestAnimationFrame(() => resolve()))
  )
}
