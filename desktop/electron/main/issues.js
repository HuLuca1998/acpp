/**
 * 菜单栏的 issue 清单：从本机 acp-server 的 GitHub 页接口拉「分配给我」的
 * issue（adr-023），缓存在壳里供菜单现 build 时用。
 *
 * 菜单弹出是同步的、拉数据是异步的，所以两者刻意解耦：菜单永远画上一次
 * 拉到的清单，弹出时顺手再拉一次给下次用；另外按固定间隔在后台刷新，
 * 用户不点也能保持新鲜。服务没起来时清单为空，菜单里说明原因。
 */

import { PORT } from "./server.js"

/** 菜单里最多列多少条：菜单栏下拉不是列表页，超过一屏没人往下翻。 */
export const MENU_LIMIT = 15

const REFRESH_MS = 3 * 60 * 1000
const FETCH_TIMEOUT_MS = 8000

export class IssueFeed {
  constructor({ server }) {
    this.server = server
    /** @type {{ items: Array<object>, total: number, fetchedAt: string|null, error: string|null }} */
    this.snapshot = { items: [], total: 0, fetchedAt: null, error: null }
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

  async fetchOnce(force) {
    if (this.server.state !== "running") {
      this.snapshot = { ...this.snapshot, items: [], total: 0, error: "服务未运行" }
      return
    }
    const url = `http://127.0.0.1:${PORT}/api/github/issues?pageSize=${MENU_LIMIT}${force ? "&refresh=1" : ""}`
    try {
      const res = await fetch(url, { signal: AbortSignal.timeout(FETCH_TIMEOUT_MS) })
      const body = await res.json()
      if (!res.ok) throw new Error(body?.error || `HTTP ${res.status}`)
      const data = body.data
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
