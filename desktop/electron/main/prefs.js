import { execFileSync } from "node:child_process"
import fs from "node:fs"
import path from "node:path"

import { app } from "electron"

/**
 * 壳自己的偏好存储。
 *
 * Swift 版存在 UserDefaults（domain `app.acpp.console`），Electron 没有等价物，
 * 落成 userData 下一个 JSON。首次运行会把旧 UserDefaults 里的值搬过来一次——
 * 老用户升级后不该发现「允许局域网访问」自己关了，那看着像 bug。
 *
 * 注意与 LaunchPreferences 的分工：开机自启的真实状态在系统登录项里，不存这儿
 * （见 launch-prefs.js 的说明），这里只有壳自己的两个开关。
 */

const DEFAULTS = {
  lanShareEnabled: false,
  startMinimized: false,
  /** 窗口位置与大小，对应 Swift 版的 setFrameAutosaveName。 */
  windowBounds: { width: 1280, height: 820 },
  /** 迁移标记：只从 UserDefaults 搬一次，之后用户在新壳里的选择说了算。 */
  migratedFromDefaults: false,
}

let cache = null
let filePath = null

function file() {
  if (!filePath) filePath = path.join(app.getPath("userData"), "prefs.json")
  return filePath
}

function load() {
  if (cache) return cache
  try {
    cache = { ...DEFAULTS, ...JSON.parse(fs.readFileSync(file(), "utf8")) }
  } catch {
    cache = { ...DEFAULTS }
  }
  if (!cache.migratedFromDefaults) {
    Object.assign(cache, readLegacyDefaults())
    cache.migratedFromDefaults = true
    save()
  }
  return cache
}

function save() {
  try {
    fs.mkdirSync(path.dirname(file()), { recursive: true })
    fs.writeFileSync(file(), JSON.stringify(cache, null, 2))
  } catch (err) {
    console.error("prefs: 写入失败", err)
  }
}

/**
 * 从 Swift 版的 UserDefaults 读一次旧值。读不到就算了——全新安装本来就没有，
 * 不值得为此报错。两个 key 的大小写沿用 Swift 那边的写法。
 */
function readLegacyDefaults() {
  const out = {}
  const read = (key) => {
    try {
      const v = execFileSync("/usr/bin/defaults", ["read", "app.acpp.console", key], {
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
      })
      return v.trim() === "1"
    } catch {
      return null
    }
  }
  const lan = read("LanShareEnabled")
  if (lan !== null) out.lanShareEnabled = lan
  const min = read("acpp.startMinimized")
  if (min !== null) out.startMinimized = min
  return out
}

export const prefs = {
  get lanShareEnabled() {
    return load().lanShareEnabled
  },
  set lanShareEnabled(on) {
    load().lanShareEnabled = on
    save()
  },
  get startMinimized() {
    return load().startMinimized
  },
  set startMinimized(on) {
    load().startMinimized = on
    save()
  },
  get windowBounds() {
    return { ...DEFAULTS.windowBounds, ...load().windowBounds }
  },
  set windowBounds(bounds) {
    load().windowBounds = bounds
    save()
  },
}
