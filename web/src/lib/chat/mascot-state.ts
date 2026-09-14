import { isToolActive, type ChatState } from "@/lib/chat/chat-events"

/**
 * 吉祥物的表情态。**全部从已有的聊天状态派生，后端不加任何字段**。
 *
 * 两个态刻意不从这里产出：`typing` 由输入卡本地升级（草稿的订阅收在
 * composer.tsx 里，拿到这一层就等于让整个面板跟着每个按键重渲），
 * `done` 是吉祥物自己的瞬时态（一轮干完笑一下，见 mascot.tsx）。
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
