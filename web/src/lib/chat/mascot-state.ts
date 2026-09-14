import { isToolActive, type ChatState } from "@/lib/chat/chat-events"

/**
 * 吉祥物的表情态。**全部从已有的聊天状态派生，后端不加任何字段**。
 *
 有三类态刻意不从这里产出——它们与聊天状态无关，只跟「人」有关：
 * `typing` 由输入卡本地升级（草稿的订阅收在 composer.tsx 里，拿到这一层
 * 就等于让整个面板跟着每个按键重渲），`done` 是一轮干完的瞬时态，
 * `dozing` / `asleep` / `waking` 是没人理它之后发呆、睡着、被叫醒。后几者见
 * hooks/use-mascot-face.ts。
 */
export type MascotState =
  | "idle"
  | "typing"
  | "thinking"
  | "reading"
  | "working"
  | "replying"
  | "waiting"
  | "done"
  | "dozing"
  | "asleep"
  | "waking"
  | "error"
  | "cancelled"
  | "offline"

/** 派生只用得上这几个字段；收窄入参，这段逻辑才能单独读懂、单独改。 */
export type MascotInput = Pick<
  ChatState,
  | "busy"
  | "connected"
  | "loading"
  | "streamingText"
  | "liveTools"
  | "pendingPermissions"
  | "elicitation"
  | "error"
  | "stopReason"
  | "lastUsage"
>

/** 「在查资料」的工具类：看和搜，与动手改东西分开（kind 取值见 tool-call.tsx 的图标表）。 */
const LOOKUP_KINDS = new Set(["read", "search", "fetch"])

/**
 * 聊天状态 → 表情态。优先级照规范 §5.3：出错 > 阻塞等人 > 运行中 > 其他。
 *
 * 「等用户裁决」排在运行中之前是因为它是**阻塞**——agent 已经停在那儿
 * 等人了，这时候还摆一张干活的脸是在骗人。
 *
 * 「运行中」又排在断线之前，这条是实测换来的：草稿页交棒到会话页那一下，
 * 轮已经乐观地开跑（busy=true）但 SSE 还没连上（connected=false），按
 * 断线优先的话每次按下发送都要先闪一张睡脸。而且轮在跑时 connected 掉了
 * 本来就是「正在重连」——那一轮在后端还活着，摆干活的脸比摆睡脸更准；
 * 真断了会有 error，它在最前面。
 */
export function mascotStateOf(c: MascotInput): MascotState {
  // 首次加载时 connected 还是 false，这时候摆断线脸纯属误报。
  if (c.loading) return "idle"
  if (c.error) return "error"
  if (c.pendingPermissions.length > 0 || c.elicitation) return "waiting"
  if (c.busy) {
    // 最后一个没跑完的工具调用就是「此刻在干的事」，与消息流折叠头同一个口径。
    const tool = c.liveTools.findLast(isToolActive)
    if (tool) {
      if (tool.kind === "think") return "thinking"
      return LOOKUP_KINDS.has(tool.kind) ? "reading" : "working"
    }
    // 正文在流就是在说话；一轮刚开始什么都还没来时统一算思考。
    return c.streamingText ? "replying" : "thinking"
  }
  if (!c.connected) return "offline"
  // stopReason 在 reducer 里已经把 end_turn 归零，剩下的都是没正常收尾。
  if (c.stopReason) return "cancelled"
  return "idle"
}

/** 正在跑一轮的那几个态：轮末的「任务完成」表情与引擎时钟的开关都看它。 */
export const MASCOT_BUSY: ReadonlySet<MascotState> = new Set<MascotState>([
  "thinking",
  "reading",
  "working",
  "replying",
])

/**
 * 气泡要说的那句话。`null` 表示没什么好说的——**这是默认**：气泡不是常驻
 * 状态栏，只在它真有信息的时候冒出来。
 *
 * `text` 是 agent 给的原文（工具标题、权限标题、错误），不翻译也不改写：
 * 那是现场信息，替它组织语言只会把细节磨掉。
 */
export type MascotSpeech =
  | { kind: "thinking" }
  | { kind: "tool"; text: string }
  | { kind: "waiting"; text?: string }
  | { kind: "asking" }
  | { kind: "done"; tokens?: number }
  | { kind: "error"; text: string }
  | { kind: "cancelled" }
  | { kind: "offline" }

/**
 * 这几种话不会自己消失：它们都在等人处理，气泡替它们把「还没完」挂在
 * 眼前。其余的说完一会儿就收回去，不占着屏幕。
 */
export const MASCOT_SPEECH_PERSISTS: ReadonlySet<MascotSpeech["kind"]> =
  new Set<MascotSpeech["kind"]>(["waiting", "asking", "error", "offline"])

/**
 * 只有这两种话需要「沉一下再说」：一轮里 思考 / 干活 会来回切好几次，
 * 每次都立刻弹就成了闪灯。
 *
 * 其余的（干完、中止、出错、等你裁决）都是一次性的结论，**必须立刻说**
 * ——实测「搞定 · 113.7k token」被 900ms 防抖吃掉大半，只闪 0.4 秒就没了。
 */
export const MASCOT_SPEECH_SETTLES: ReadonlySet<MascotSpeech["kind"]> = new Set<
  MascotSpeech["kind"]
>(["thinking", "tool"])

/**
 * 表情态 + 现场数据 → 气泡内容。
 *
 * 「正在回复」刻意不说话：正文本身就在屏幕上流，气泡重复一遍毫无信息量。
 * 空闲、打字、发呆、睡觉同理——没事就别开口。
 */
export function mascotSpeechOf(
  state: MascotState,
  c: MascotInput
): MascotSpeech | null {
  switch (state) {
    case "thinking":
      return { kind: "thinking" }
    case "reading":
    case "working": {
      // 说具体那件事：claude 的工具标题是「Read mascot.tsx」这种人话，
      // 比我们自己编一句「正在干活」有用得多；codex 可能没有，退回思考。
      const tool = c.liveTools.findLast(isToolActive)
      const title = tool?.title?.trim()
      return title ? { kind: "tool", text: title } : { kind: "thinking" }
    }
    case "waiting": {
      if (c.pendingPermissions.length === 0) return { kind: "asking" }
      // 只有 claude 带 title（如 "Write hello.txt"），codex 为空就只说在等。
      const title = c.pendingPermissions[0]?.title?.trim()
      return { kind: "waiting", text: title || undefined }
    }
    case "done":
      return { kind: "done", tokens: c.lastUsage?.totalTokens }
    case "error":
      return { kind: "error", text: c.error ?? "" }
    case "cancelled":
      return { kind: "cancelled" }
    case "offline":
      return { kind: "offline" }
    default:
      return null
  }
}
