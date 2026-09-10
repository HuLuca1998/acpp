// 托盘弹层的渲染：拿快照画列表，点行打开 GitHub，点编号复制编号。
// 没有框架：一屏十几行，手写 DOM 比拉一套依赖便宜得多。

/** GitHub 单选字段的色名（Priority / 看板列共用一套）→ 实际颜色，与 codex-ui 同表。 */
const GH_COLORS = {
  GRAY: "#8b949e",
  BLUE: "#58a6ff",
  GREEN: "#3fb950",
  YELLOW: "#d29922",
  ORANGE: "#db6d28",
  RED: "#f85149",
  PINK: "#db61a2",
  PURPLE: "#a371f7",
}

function ghColor(name) {
  return GH_COLORS[String(name ?? "").toUpperCase()] ?? null
}

/** 行首点的颜色：优先级色优先（与 GitHub 上的 Priority 一致），没设则退到第一个标签色。 */
function dotColor(issue) {
  const p = ghColor(issue.priorityColor)
  if (p) return p
  const label = issue.labels?.[0]?.color
  return label ? `#${label}` : GH_COLORS.GRAY
}

function relTime(iso) {
  if (!iso) return ""
  const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000))
  if (s < 60) return "刚刚更新"
  if (s < 3600) return `${Math.floor(s / 60)} 分钟前更新`
  if (s < 86400) return `${Math.floor(s / 3600)} 小时前更新`
  return `${Math.floor(s / 86400)} 天前更新`
}

function el(tag, cls, text) {
  const node = document.createElement(tag)
  if (cls) node.className = cls
  if (text != null) node.textContent = text
  return node
}

const listEl = document.getElementById("list")
const countEl = document.getElementById("count")
const metaEl = document.getElementById("meta")
const refreshBtn = document.getElementById("refresh")

function render(snap) {
  listEl.replaceChildren()
  const items = snap.items ?? []
  countEl.textContent = items.length
    ? snap.total > items.length
      ? `${snap.total} · 前 ${items.length}`
      : String(snap.total)
    : ""
  metaEl.textContent = snap.running ? relTime(snap.fetchedAt) : ""

  if (!snap.running) {
    listEl.append(el("div", "info", "服务未运行——右键菜单栏图标可重启"))
  } else if (items.length === 0) {
    const why = snap.error
      ? `拉取失败：${snap.error}`
      : snap.watched && snap.watched.length === 0
        ? "还没有关注仓库，去 GitHub 页挑几个"
        : "没有分配给你的 issue"
    listEl.append(el("div", "info", why))
  } else {
    for (const it of items) listEl.append(row(it))
    if (snap.error) listEl.append(el("div", "info", `部分仓库拉取失败：${snap.error}`))
  }
  reportSize()
}

function row(it) {
  const r = el("div", "row")
  r.title = `${it.repo}#${it.number}\n${it.title}\n点击打开 GitHub；点编号复制 ${it.number}`
  const dot = el("span", "dot")
  dot.style.background = dotColor(it)
  // 编号是独立热区：点它复制纯数字编号，不冒泡到行（行是打开 GitHub）
  const num = el("button", "num", `#${it.number}`)
  num.title = `复制 ${it.number}`
  num.addEventListener("click", (e) => {
    e.stopPropagation()
    void window.acppTray.copy(String(it.number))
    r.classList.add("copied")
    setTimeout(() => r.classList.remove("copied"), 1200)
  })
  r.append(dot, num, el("span", "repo", it.repo.split("/").pop()))
  if (it.status) {
    const st = el("span", "status", it.status)
    const c = ghColor(it.statusColor)
    if (c) st.style.setProperty("--st", c)
    r.append(st)
  }
  r.append(el("span", "title", it.title), el("span", "copied-msg", `✓ 已复制 #${it.number}`))
  r.addEventListener("click", () => void window.acppTray.open(it.url))
  return r
}

/** 高度跟内容走：算完 DOM 让壳把窗口拉到正好。 */
function reportSize() {
  const h = document.querySelector(".panel").getBoundingClientRect().height
  void window.acppTray.size(Math.ceil(h))
}

refreshBtn.addEventListener("click", async () => {
  refreshBtn.classList.add("spin")
  try {
    render(await window.acppTray.refresh())
  } finally {
    refreshBtn.classList.remove("spin")
  }
})
document.getElementById("open-app").addEventListener("click", () => void window.acppTray.openApp())
document.getElementById("open-all").addEventListener("click", () => void window.acppTray.openApp("/github"))
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") void window.acppTray.hide()
})

window.acppTray.onSnapshot(render)
window.acppTray.get().then(render)
