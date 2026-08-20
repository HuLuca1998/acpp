import { useTranslation } from "react-i18next"

import { Hint } from "@/components/hint"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import { isDestructive } from "@/lib/mcp-tool"
import type { McpServer, McpTool, McpToolStat } from "@/types/acp"
import {
  CircleSlashIcon,
  EyeIcon,
  PencilLineIcon,
  PlugIcon,
} from "lucide-react"

/**
 * 工具清单：按 MCP server 分组列出工具。
 *
 * 每条工具旁边标两件事——**会不会改数据**（读/写徽标）与**被调用过几次**。
 * 后者是这个页面最有价值的一列：一个从没被 AI 调用过的工具，问题多半
 * 出在描述而不是实现上，那正是这里该暴露出来的信号。
 */
export function ToolCatalog({
  servers,
  stats,
  selected,
  onSelect,
}: {
  servers: McpServer[]
  stats: McpToolStat[]
  selected?: McpTool
  onSelect: (server: McpServer, tool: McpTool) => void
}) {
  const { t } = useTranslation()

  return (
    <div className="flex flex-col gap-4">
      {servers.map((server) => (
        <div key={server.name} className="flex flex-col gap-1">
          {/* 小节头：server 名 + 挂没挂给 agent。轻量一行，不做成卡片。 */}
          <div className="flex items-center gap-1.5 px-2 pb-1">
            <PlugIcon className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate font-mono text-xs font-medium">
              {server.name}
            </span>
            {server.mounted ? null : (
              <Hint
                label={t("tools.catalog.unmounted")}
                desc={t("tools.catalog.unmountedDesc")}
              >
                <Badge variant="outline" className="gap-1 text-[11px]">
                  <CircleSlashIcon className="size-3" />
                  {t("tools.catalog.unmountedShort")}
                </Badge>
              </Hint>
            )}
          </div>

          {server.tools.map((tool) => (
            <ToolRow
              key={tool.name}
              tool={tool}
              stat={stats.find(
                (s) => s.server === server.name && s.tool === tool.name
              )}
              active={selected?.name === tool.name}
              onSelect={() => onSelect(server, tool)}
            />
          ))}
        </div>
      ))}
    </div>
  )
}

function ToolRow({
  tool,
  stat,
  active,
  onSelect,
}: {
  tool: McpTool
  stat?: McpToolStat
  active: boolean
  onSelect: () => void
}) {
  const { t } = useTranslation()
  const writes = isDestructive(tool)

  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "flex w-full flex-col gap-0.5 rounded-md px-2 py-1.5 text-left transition-colors",
        "hover:bg-accent",
        active && "bg-accent"
      )}
    >
      <div className="flex items-center gap-1.5">
        {writes ? (
          <PencilLineIcon className="size-3.5 shrink-0 text-warning" />
        ) : (
          <EyeIcon className="size-3.5 shrink-0 text-muted-foreground" />
        )}
        <span className="min-w-0 flex-1 truncate font-mono text-sm">
          {tool.name}
        </span>
        {/* 没被调用过就不显示 0：一个刺眼的 0 会被读成「出问题了」，
            而真相只是「还没人用」——那是空缺，不是异常。 */}
        {stat && stat.count > 0 ? (
          <Hint
            label={t("tools.catalog.callCount", { count: stat.count })}
            desc={
              stat.errorCount > 0
                ? t("tools.catalog.errorCount", { count: stat.errorCount })
                : undefined
            }
          >
            <span
              className={cn(
                "shrink-0 text-xs tabular-nums",
                stat.errorCount > 0 ? "text-warning" : "text-muted-foreground"
              )}
            >
              {stat.count}
            </span>
          </Hint>
        ) : null}
      </div>
      <span className="line-clamp-1 pl-5 text-xs text-muted-foreground">
        {tool.description}
      </span>
    </button>
  )
}
