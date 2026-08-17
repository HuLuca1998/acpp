import type { SlashCommand } from "@/types/acp"

/**
 * 本地斜杠命令：前端自己拦截执行的命令，不发给 agent。
 *
 * 与 agent 的命令清单混在同一个补全菜单里（用户不该关心一条命令是谁
 * 实现的），但走完全不同的路径——本地命令的结果只渲染给人看，不进对话、
 * 不消耗 token、不用等 agent 响应。查一眼有哪些库属于「顺手看看」，
 * 不该为此惊动模型。
 */

/** 本地命令名，加进补全清单时带 local 标记以便与 agent 命令区分。 */
export const LOCAL_COMMANDS = {
  /** 查看当前项目的数据库：`/db`、`/db dev`、`/db dev mydb`。 */
  db: "db",
} as const

export interface ParsedLocalCommand {
  name: string
  /** 命令名之后的参数（原样，已 trim）。 */
  args: string
}

/**
 * 解析一条输入是不是本地命令。只认「整条输入以 /<命令名> 开头」，
 * 消息正文里偶然出现的斜杠不受影响。
 */
export function parseLocalCommand(input: string): ParsedLocalCommand | null {
  const text = input.trim()
  if (!text.startsWith("/")) return null
  const [head, ...rest] = text.slice(1).split(/\s+/)
  if (!Object.hasOwn(LOCAL_COMMANDS, head)) return null
  return { name: head, args: rest.join(" ").trim() }
}

/**
 * 把本地命令并进 agent 的命令清单供补全用。
 * 本地命令排在前面：它们响应最快，也最常是「先看一眼」的起点。
 */
export function withLocalCommands(
  commands: SlashCommand[],
  descriptions: Record<string, string>
): SlashCommand[] {
  const local = Object.values(LOCAL_COMMANDS).map((name) => ({
    name,
    description: descriptions[name] ?? "",
  }))
  return [...local, ...commands]
}
