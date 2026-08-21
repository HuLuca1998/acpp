/**
 * 页内通知形式——浏览器这一侧唯一可用的手段。
 *
 * 局域网访客拿不到系统通知：macOS 通知只发到 owner 那台机器，而浏览器的
 * Web Notification 要求 secure context，局域网是明文 http（README §安全
 * 姿态），拿不到权限。所以只剩页面自己够得着的东西：标题是标签页被切走后
 * 唯一还看得见的地方，声音是人在看别的窗口时唯一还传得到的信号。
 */

/** 标题闪烁的切换间隔。太快像故障，太慢会被整段忽略。 */
const FLASH_MS = 1200

let flashTimer: ReturnType<typeof setInterval> | null = null
let baseTitle: string | null = null

/**
 * 用户此刻是不是正看着这个页面。
 *
 * 两个条件都要：标签页可见（没被切走），且窗口有焦点（不是被别的 app 盖在
 * 后面）。只看 visibilityState 会漏掉「浏览器开着但人在别的 app 里」——
 * 那恰恰是最需要通知的情形。
 */
export function isUserWatching(): boolean {
  if (typeof document === "undefined") return false
  return document.visibilityState === "visible" && document.hasFocus()
}

/** 开始标题闪烁。重复调用只会换文案，不会叠出第二个计时器。 */
export function flashTitle(text: string) {
  if (typeof document === "undefined") return
  // 先停一次：baseTitle 必须是真正的原始标题，不能是上一轮闪到一半的那个。
  stopFlashTitle()
  baseTitle = document.title
  let on = false
  flashTimer = setInterval(() => {
    on = !on
    document.title = on ? text : (baseTitle ?? "")
  }, FLASH_MS)
}

/** 停止闪烁并把标题还原。用户回到页面就该停——他已经看见了。 */
export function stopFlashTitle() {
  if (flashTimer === null) return
  clearInterval(flashTimer)
  flashTimer = null
  if (baseTitle !== null) document.title = baseTitle
  baseTitle = null
}

interface LegacyAudioWindow extends Window {
  webkitAudioContext?: typeof AudioContext
}

let audioCtx: AudioContext | null = null

/**
 * 提示音。用 Web Audio 现场合成，不打包音频文件——省掉一个二进制资源，
 * 也不用担心它在局域网 http 下加载失败。
 *
 * 浏览器的自动播放策略要求页面上发生过用户交互，否则 AudioContext 起不来。
 * 这里静默退让：声音是锦上添花，没有它通知照样在，不该为此报错或重试。
 */
export function playChime() {
  try {
    const Ctor = window.AudioContext ?? (window as LegacyAudioWindow).webkitAudioContext
    if (!Ctor) return
    audioCtx ??= new Ctor()
    if (audioCtx.state === "suspended") void audioCtx.resume()

    const now = audioCtx.currentTime
    // 两声短促的上行，像一句「看一眼」，不像警报。
    for (const [offset, freq] of [
      [0, 880],
      [0.12, 1174.66],
    ] as const) {
      const osc = audioCtx.createOscillator()
      const gain = audioCtx.createGain()
      osc.type = "sine"
      osc.frequency.value = freq
      // 包络：瞬间起、快速衰减。直接开关振荡器会有「啪」的爆音。
      gain.gain.setValueAtTime(0, now + offset)
      gain.gain.linearRampToValueAtTime(0.12, now + offset + 0.01)
      gain.gain.exponentialRampToValueAtTime(0.0001, now + offset + 0.18)
      osc.connect(gain).connect(audioCtx.destination)
      osc.start(now + offset)
      osc.stop(now + offset + 0.2)
    }
  } catch {
    // 音频起不来不该影响通知本身。
  }
}
