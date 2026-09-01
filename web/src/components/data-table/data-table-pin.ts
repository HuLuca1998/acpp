import { useLayoutEffect, useRef, useState } from "react"

import { cn } from "@/lib/utils"

/**
 * 固定列（`meta.pin`）的落地：算偏移、判断边缘线该不该出现。
 *
 * 规矩是「关键信息左固定，操作按钮右固定」。表一宽，横向滚动就把这两样
 * 一起卷走：中间的字段读到一半认不出是哪一行的，右边的编辑/删除得先滚
 * 回去才点得到。
 *
 * 偏移只能实测。列宽由内容撑开（表格没开 columnSizing，`getSize()` 一律
 * 是那个 150 的默认值），所以第二个固定列该往里挪多少，只有量过表头才
 * 知道。
 */

export type PinSide = "left" | "right"

/** 只需要这两样：id 用来存偏移，pin 决定钉哪边。 */
type PinColumn = { id: string; pin?: PinSide }

/**
 * 固定列的公共样式。
 *
 * 不透明底是硬要求：`position: sticky` 只把这一格提到上层，滚过来的单元格
 * 会从它下面透出来。垫在 `-z-10` 而不是直接给 `bg-card`，是为了把这一格
 * 自身的背景层空出来给行 hover 用——两层叠起来才能既挡住下面的内容、又
 * 跟着整行一起变色。
 *
 * 底色取 `--row-bg`（缺省是卡片底色，本项目的列表表格一律待在 `Card` 里）。
 * 不能写死：行有自己的底色时——比如数据库页展开的那一行——固定列若还铺
 * 卡片底，那一行就会从固定列这里断成两截。所以 `rowClassName` 要改行底色
 * 的话，设 `--row-bg` 而不是直接给 `bg-*`。
 *
 * hover 走单元格自身的背景层，叠在这层不透明底之上，与其他列算出来的是
 * 同一个颜色。
 */
const PIN_BASE = cn(
  "sticky z-10",
  "before:absolute before:inset-0 before:-z-10 before:bg-[var(--row-bg,var(--card))]",
  "[tr:hover_&]:bg-muted/50"
)

/**
 * @param ready 表格此刻是否真的画出来了。加载中与空列表整个让给三态壳，
 *   那时连 `<table>` 都没有——不把它放进依赖，首屏拿到的就是两个空 ref，
 *   而列结构不变、effect 不会再跑第二次，滚动监听于是永远装不上。
 */
export function usePinnedColumns(columns: PinColumn[], ready: boolean) {
  const scrollerRef = useRef<HTMLDivElement>(null)
  const headRowRef = useRef<HTMLTableRowElement>(null)
  const [offsets, setOffsets] = useState<Record<string, number>>({})
  const [edge, setEdge] = useState({ start: false, end: false })

  // 列的构成变了（显隐、换页、改语言）才需要重测。直接依赖 columns 不行
  // ——每次渲染都是新数组，effect 会跟着每帧重订阅一次。
  const key = columns.map((c) => `${c.id}:${c.pin ?? ""}`).join(",")

  useLayoutEffect(() => {
    const scroller = scrollerRef.current
    const head = headRowRef.current
    if (!scroller || !head) return

    const sync = () => {
      const cells = Array.from(head.children) as HTMLElement[]
      const next: Record<string, number> = {}
      // 宽度取 rect 而不是 offsetWidth：后者四舍五入到整数，累加出来的
      // 偏移会比实际少半个像素——两个固定列之间就裂开一条缝，滚动的内容
      // 正好从缝里穿过去。
      let acc = 0
      columns.forEach((c, i) => {
        if (c.pin !== "left" || !cells[i]) return
        next[c.id] = acc
        acc += cells[i].getBoundingClientRect().width
      })
      acc = 0
      for (let i = columns.length - 1; i >= 0; i--) {
        const c = columns[i]
        if (c.pin !== "right" || !cells[i]) continue
        next[c.id] = acc
        acc += cells[i].getBoundingClientRect().width
      }
      setOffsets((prev) => (sameOffsets(prev, next) ? prev : next))

      // 边缘线只在真的挡住了东西时才画——没溢出的表格上凭空多两条竖线，
      // 只会让人以为那是分组。1px 容差是给缩放与小数宽度留的：那种情况下
      // scrollLeft 到不了整数的尽头。
      const start = scroller.scrollLeft > 1
      const end =
        scroller.scrollLeft + scroller.clientWidth < scroller.scrollWidth - 1
      setEdge((prev) =>
        prev.start === start && prev.end === end ? prev : { start, end }
      )
    }

    sync()
    // 逐格观察而不是只盯表头行：换页后某一列的内容变长时，表格总宽常常
    // 一点不变（本来就撑满或本来就溢出），只有那一格自己在变。
    const ro = new ResizeObserver(sync)
    ro.observe(scroller)
    for (const cell of head.children) ro.observe(cell)
    scroller.addEventListener("scroll", sync, { passive: true })
    return () => {
      ro.disconnect()
      scroller.removeEventListener("scroll", sync)
    }
    // columns 每次渲染都是新数组，用 key 代表「构成变了」。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, ready])

  // 同侧最靠内的那一列——左侧取最后一个，右侧取第一个。
  const inner: Record<PinSide, string | undefined> = {
    left: columns.filter((c) => c.pin === "left").at(-1)?.id,
    right: columns.find((c) => c.pin === "right")?.id,
  }

  return {
    scrollerRef,
    headRowRef,
    /** 贴到表头与单元格上的定位类与偏移，非固定列返回空对象。 */
    pinProps(id: string, pin?: PinSide) {
      if (!pin) return {}
      // 边缘线只画在同侧**最靠内**的那一列上：那里才是「固定的到此为止，
      // 后面是滚动的」这条界。每个固定列都画，中间会多出几条竖线，看着
      // 像把表格切成了好几块。
      const shown =
        id === inner[pin] && (pin === "left" ? edge.start : edge.end)
      return {
        className: cn(
          PIN_BASE,
          shown && (pin === "left" ? "border-r" : "border-l")
        ),
        style:
          pin === "left"
            ? { left: offsets[id] ?? 0 }
            : { right: offsets[id] ?? 0 },
      }
    },
  }
}

function sameOffsets(
  a: Record<string, number>,
  b: Record<string, number>
): boolean {
  const ka = Object.keys(a)
  if (ka.length !== Object.keys(b).length) return false
  return ka.every((k) => a[k] === b[k])
}
