/**
 * 用指定的 Chrome 账号（profile）打开外链。
 *
 * 私有仓库往往只有某一个 Google 账号有权限，不指定账号时 Chrome 用「上次
 * 使用的」那个，打开 issue 直接 404。而且必须直接执行 Chrome 二进制并传
 * `--profile-directory`：`open -a` / shell.openExternal 在 Chrome 已运行时只是
 * 把 URL 转给现有实例，附带的启动参数被整段忽略，链接照样落在上次用的账号里。
 * 直接跑二进制则由 Chrome 自己转发到指定账号的窗口（未运行时就是正常启动）。
 *
 * 与 codex-ui 的做法一致（那边是 Swift 的 Process + Go 读 Local State）。
 */

import { spawn } from "node:child_process"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"

import { shell } from "electron"

const CHROME_APP = "/Applications/Google Chrome.app"
const CHROME_BIN = path.join(CHROME_APP, "Contents/MacOS/Google Chrome")
const LOCAL_STATE = path.join(
  os.homedir(),
  "Library/Application Support/Google/Chrome/Local State"
)

/**
 * 读 Chrome 的 Local State，列出本机所有账号。读不到（没装 Chrome / 从未
 * 启动过 / 格式变了）返回空数组，菜单据此不出账号子菜单。
 * @returns {Array<{dir: string, name: string, email: string, last: boolean}>}
 */
export function chromeProfiles() {
  let state
  try {
    state = JSON.parse(fs.readFileSync(LOCAL_STATE, "utf8"))
  } catch {
    return []
  }
  const cache = state?.profile?.info_cache
  if (!cache || typeof cache !== "object") return []
  const lastUsed = state.profile.last_used
  const out = Object.entries(cache).map(([dir, info]) => ({
    dir,
    name: String(info?.name ?? "").trim() || dir,
    email: String(info?.user_name ?? "").trim(),
    last: dir === lastUsed,
  }))
  // Default 排最前，其余按 "Profile N" 的序号升序。
  out.sort((a, b) => {
    if ((a.dir === "Default") !== (b.dir === "Default")) return a.dir === "Default" ? -1 : 1
    const na = Number(a.dir.replace(/^Profile /, ""))
    const nb = Number(b.dir.replace(/^Profile /, ""))
    if (Number.isFinite(na) && Number.isFinite(nb)) return na - nb
    return a.dir.localeCompare(b.dir)
  })
  return out
}

/**
 * 打开链接：指定了账号且 Chrome 在，就直接跑二进制；否则交给系统默认浏览器。
 * @param {string} url
 * @param {string} profileDir Chrome 账号目录名（Default / Profile 1…），空 = 不指定
 */
export function openURL(url, profileDir) {
  const dir = String(profileDir ?? "").trim()
  if (dir && fs.existsSync(CHROME_BIN)) {
    try {
      const child = spawn(CHROME_BIN, [`--profile-directory=${dir}`, url], {
        detached: true,
        stdio: "ignore",
      })
      child.unref()
      return
    } catch (err) {
      console.error("用指定 Chrome 账号打开失败，回退系统默认浏览器:", err)
    }
  }
  void shell.openExternal(url)
}
