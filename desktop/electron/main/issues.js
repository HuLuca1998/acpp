/**
 * 菜单栏的 issue 清单：从本机 acp-server 的 GitHub 页接口拉「分配给我」的
 * issue（adr-023），缓存在壳里供菜单现 build 时用。
 *
 * 菜单弹出是同步的、拉数据是异步的，所以两者刻意解耦：菜单永远画上一次
 * 拉到的清单，弹出时顺手再拉一次给下次用；另外按固定间隔在后台刷新，
 * 用户不点也能保持新鲜。服务没起来时清单为空，菜单里说明原因。
 */

import { prefs } from "./prefs.js"
import { PORT } from "./server.js"

/** 每页多少条：一屏放得下，再多靠翻页。 */
export const PAGE_SIZE = 15

/**
 * 可选的排序 → 后端查询参数。状态排序按看板列从左到右（待处理在前），
 * 同列再按优先级；更新时间只有「最近的在前」一个方向有用。
 */
export const SORTS = {
  status: "sort=status",
  priority: "sort=priority",
  updated: "sort=updated&order=desc",
}

const REFRESH_MS = 3 * 60 * 1000
const FETCH_TIMEOUT_MS = 8000

export class IssueFeed {
  constructor({ server }) {
    this.server = server
    /** @type {{ items: Array<object>, total: number, fetchedAt: string|null, error: string|null }} */
    this.snapshot = { items: [], total: 0, fetchedAt: null, error: null }
    /** 当前页码：只活在本次运行里，弹层每次打开回到第一页。 */
    this.page = 1
    this.timer = null
    this.inflight = null
    /** 快照更新后的回调，弹层据此重画；壳装配时赋值。 */
    this.onChange = null
  }

  /** 起后台刷新。服务起来前拉不到东西，但拉一次的代价只是一个失败的 fetch。 */
  start() {
    this.stop()
    this.timer = setInterval(() => this.refresh(), REFRESH_MS)
    void this.refresh()
  }

  stop() {
    if (this.timer) clearInterval(this.timer)
    this.timer = null
  }

  /**
   * 拉一次；并发调用共用同一个请求。默认只读后端缓存（后端自己每 3 分钟
   * 刷一轮 GitHub），force 才让后端立刻去 GitHub 拉——菜单里的「刷新」用。
   */
  refresh({ force = false } = {}) {
    if (this.inflight) return this.inflight
    this.inflight = this.fetchOnce(force).finally(() => {
      this.inflight = null
      this.onChange?.()
    })
    return this.inflight
  }

  get sort() {
    return SORTS[prefs.issueSort] ? prefs.issueSort : "status"
  }

  /** 换排序：记进偏好，回到第一页重拉。 */
  setSort(sort) {
    if (!SORTS[sort]) return Promise.resolve()
    prefs.issueSort = sort
    return this.goto(1)
  }

  /** 翻到某页。在途请求会拿旧页码，等它结束再拉一次新页。 */
  async goto(page) {
    this.page = Math.max(1, Math.floor(page) || 1)
    await this.inflight
    return this.refresh()
  }

  async fetchOnce(force) {
    if (this.server.state !== "running") {
      this.snapshot = { ...this.snapshot, items: [], total: 0, error: "服务未运行" }
      return
    }
    const page = this.page
    const url =
      `http://127.0.0.1:${PORT}/api/github/issues?page=${page}&pageSize=${PAGE_SIZE}&${SORTS[this.sort]}` +
      (force ? "&refresh=1" : "")
    try {
      const res = await fetch(url, { signal: AbortSignal.timeout(FETCH_TIMEOUT_MS) })
      const body = await res.json()
      if (!res.ok) throw new Error(body?.error || `HTTP ${res.status}`)
      const data = body.data
      // 清单变短后当前页可能已越界：退到最后一页，下次刷新补上内容。
      const pages = Math.max(1, Math.ceil((data.total ?? 0) / PAGE_SIZE))
      if (page > pages) {
        this.page = pages
        return this.fetchOnce(false)
      }
      this.snapshot = {
        items: data.items ?? [],
        total: data.total ?? 0,
        fetchedAt: data.fetchedAt ?? null,
        // 部分仓库拉失败只在菜单尾巴上提一句，不清空已有的清单。
        error: data.errors?.length ? data.errors.join("；") : null,
        watched: data.watched ?? [],
      }
    } catch (err) {
      this.snapshot = { ...this.snapshot, error: err?.message || String(err) }
    }
  }
}
