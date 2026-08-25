import { execFileSync, spawn } from "node:child_process"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"

import { app } from "electron"

import { primaryIPv4 } from "./lan.js"
import { prefs } from "./prefs.js"

/**
 * 管理捆绑的 acp-server 子进程：端口占用清理、启动、健康等待、局域网开关、优雅停止。
 *
 * 逻辑逐条对应原 Swift 版 ServerController，行为不变——端口策略、PATH 注入、
 * 自更新握手都是外部（dev.sh、后端 update.go）依赖的契约，换壳不该改动它们。
 */

/**
 * 桌面版固定端口。与开发态 48080 隔离：dev.sh 清理 48080 时不会误杀桌面版，
 * 桌面版清理 48090 时也不会碰开发进程（见 ADR-004）。
 *
 * `ACPP_SHELL_PORT` 只在**未打包**时生效，专治一个真实的坑：调壳代码时启动
 * 会先「清掉占 48090 的进程」，而那个进程正是用户装着的、可能正在用的
 * ACPP.app 的后端。调壳请用 `ACPP_SHELL_PORT=48091 npm start`，两边互不打扰。
 */
export const PORT =
  !app.isPackaged && process.env.ACPP_SHELL_PORT
    ? Number(process.env.ACPP_SHELL_PORT)
    : 48090

export const LOG_PATH = path.join(os.homedir(), "Library/Logs/ACPP/server.log")

const STATE = { stopped: "stopped", starting: "starting", running: "running", failed: "failed" }

export class ServerController {
  constructor() {
    this.state = STATE.stopped
    this.child = null
    /** stop() 主动停时置真，exit 回调据此区分「意外退出」。 */
    this.deliberateStop = false
  }

  get localURL() {
    return `http://127.0.0.1:${PORT}/`
  }

  get lanURL() {
    const ip = primaryIPv4()
    return ip ? `http://${ip}:${PORT}/` : null
  }

  get lanShareEnabled() {
    return prefs.lanShareEnabled
  }

  set lanShareEnabled(on) {
    prefs.lanShareEnabled = on
  }

  // MARK: - 生命周期

  /** 清掉 48090 上的遗留进程（上次强杀留下的孤儿）→ 拉起子进程 → 等健康检查。 */
  async start() {
    this.state = STATE.starting
    killPortOccupant()
    if (!this.spawn()) {
      this.state = STATE.failed
      return false
    }
    const ok = await this.waitHealthy()
    this.state = ok ? STATE.running : STATE.failed
    return ok
  }

  async restart() {
    await this.stop()
    return this.start()
  }

  /**
   * 优雅停止：SIGTERM 给后端收尾逻辑一个触发点，3 秒等不到就 SIGKILL。
   * 必须等它真的死掉再返回——退出路径上这是最后一道回收 agent 子进程的机会。
   */
  async stop() {
    this.deliberateStop = true
    const child = this.child
    if (!child || child.exitCode !== null || child.killed === true) {
      this.child = null
      this.state = STATE.stopped
      return
    }
    child.kill("SIGTERM")
    const deadline = Date.now() + 3000
    while (child.exitCode === null && Date.now() < deadline) {
      await sleep(100)
    }
    if (child.exitCode === null) child.kill("SIGKILL")
    this.child = null
    this.state = STATE.stopped
  }

  // MARK: - 子进程

  spawn() {
    this.deliberateStop = false
    const { serverPath, webDir } = resolvePaths()
    if (!fs.existsSync(serverPath)) {
      console.error("acp-server 不存在:", serverPath)
      return false
    }

    const env = { ...process.env }
    env.ACP_ADDR = `${this.lanShareEnabled ? "0.0.0.0" : "127.0.0.1"}:${PORT}`
    env.ACP_WEB_DIR = webDir
    // GUI 启动的 app 只有系统级 PATH，server 靠 PATH 拉起 claude/codex 等
    // agent 子进程——必须注入登录 shell 的 PATH，否则核心功能直接瘫痪。
    env.PATH = loginShellPATH()
    // 自更新要给壳发 TERM 才能走正常退出（回收 server 与 agent 子进程）。
    // server 不能靠 getppid() 找我们——它一旦孤儿化，那个值就是 1。
    env.ACPP_SHELL_PID = String(process.pid)

    let stdio = ["ignore", "ignore", "ignore"]
    const log = openLog()
    if (log !== null) stdio = ["ignore", log, log]

    try {
      const child = spawn(serverPath, [], { env, stdio })
      child.on("exit", () => {
        if (this.deliberateStop) return
        this.state = STATE.failed
        this.child = null
      })
      child.on("error", (err) => console.error("acp-server 启动失败:", err))
      this.child = child
      return true
    } catch (err) {
      console.error("acp-server 启动失败:", err)
      return false
    }
  }

  // MARK: - 健康检查

  async healthOK() {
    try {
      const res = await fetch(`${this.localURL}api/health`, {
        signal: AbortSignal.timeout(800),
      })
      return res.status === 200
    } catch {
      return false
    }
  }

  async waitHealthy(timeoutMs = 25_000) {
    const deadline = Date.now() + timeoutMs
    while (Date.now() < deadline) {
      if (await this.healthOK()) return true
      // 子进程已经死了就不必等满超时
      if (!this.child || this.child.exitCode !== null) return false
      await sleep(250)
    }
    return false
  }
}

// MARK: - 工具

/**
 * acp-server 与前端产物的位置。
 *
 * 打包态照 Swift 版的老位置放（`Contents/MacOS/acp-server`）——后端的自更新
 * 逻辑按 bundle 结构找壳进程，位置换了要连着改 update.go，没必要。
 * 开发态直接吃仓库的 build/ 产物，改壳代码时不用每次重新打包。
 */
function resolvePaths() {
  if (app.isPackaged) {
    const contents = path.resolve(path.dirname(process.execPath), "..")
    return {
      serverPath: path.join(contents, "MacOS/acp-server"),
      webDir: path.join(contents, "Resources/web"),
    }
  }
  const repo = findRepoRoot()
  return {
    serverPath: path.join(repo, "build/server/acp-server"),
    webDir: path.join(repo, "build/web"),
  }
}

/**
 * 开发态找仓库根。`npm start` 时 appPath 就是 desktop/electron，往上两级即可；
 * 但用 `electron <别处的脚本>` 跑自检时 appPath 会指向那个脚本的目录，所以
 * 再拿 cwd 兜一手。都找不到就返回首选，让调用方报「acp-server 不存在」——
 * 那条消息比在这里抛异常好读。
 */
function findRepoRoot() {
  const candidates = [
    path.resolve(app.getAppPath(), "../.."),
    path.resolve(process.cwd(), "../.."),
    process.cwd(),
  ]
  return (
    candidates.find((dir) => fs.existsSync(path.join(dir, "build/server/acp-server"))) ??
    candidates[0]
  )
}

function openLog() {
  try {
    fs.mkdirSync(path.dirname(LOG_PATH), { recursive: true })
    return fs.openSync(LOG_PATH, "a")
  } catch (err) {
    console.error("打开服务日志失败:", err)
    return null
  }
}

/**
 * 清理 48090 上的遗留监听进程。app 被 SIGKILL 时来不及回收子进程，
 * 下次启动按仓库端口策略处理：占口者一律清掉后在原端口重启。
 */
function killPortOccupant() {
  const listeners = () => {
    try {
      return execFileSync("/usr/sbin/lsof", ["-ti", `tcp:${PORT}`, "-sTCP:LISTEN"], {
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
      })
        .split("\n")
        .map((s) => Number(s.trim()))
        .filter((n) => Number.isInteger(n) && n > 0)
    } catch {
      return []
    }
  }

  const pids = listeners()
  if (pids.length === 0) return
  for (const pid of pids) safeKill(pid, "SIGTERM")

  const deadline = Date.now() + 2000
  while (Date.now() < deadline) {
    if (listeners().length === 0) return
    sleepSync(100)
  }
  for (const pid of listeners()) safeKill(pid, "SIGKILL")
}

function safeKill(pid, signal) {
  try {
    process.kill(pid, signal)
  } catch {
    // 已经没了就算了
  }
}

/** 登录 shell 的 PATH，取一次缓存。取不到时退回系统 PATH + Homebrew 常见位置。 */
let cachedPATH = null
function loginShellPATH() {
  if (cachedPATH) return cachedPATH
  const fallback = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
  let raw = ""
  try {
    raw = execFileSync(process.env.SHELL || "/bin/zsh", ["-lc", 'printf %s "$PATH"'], {
      encoding: "utf8",
      timeout: 3000,
      stdio: ["ignore", "pipe", "ignore"],
    })
  } catch {
    raw = ""
  }
  if (!raw.includes("/")) {
    cachedPATH = fallback
    return cachedPATH
  }
  // 系统目录必须在——登录 shell 配置再奇怪，基础命令也不能丢
  const merged = raw.split(":").filter(Boolean)
  for (const dir of ["/usr/bin", "/bin", "/usr/sbin", "/sbin"]) {
    if (!merged.includes(dir)) merged.push(dir)
  }
  cachedPATH = merged.join(":")
  return cachedPATH
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

/** 同步小睡，只在启动前清端口这一处用——那时还没有窗口，阻塞无所谓。 */
function sleepSync(ms) {
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms)
}
