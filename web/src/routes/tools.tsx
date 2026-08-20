import { useState } from "react"
import { useTranslation } from "react-i18next"

import { CallLog } from "@/components/tools/call-log"
import { ToolCatalog } from "@/components/tools/tool-catalog"
import { ToolDetail } from "@/components/tools/tool-detail"
import { Hint } from "@/components/hint"
import { ListPageStates } from "@/components/list-page-states"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useAsyncData } from "@/hooks/use-async-data"
import { api } from "@/lib/api"
import { CircleSlashIcon, WrenchIcon } from "lucide-react"

/**
 * 工具页：我方 MCP server 暴露给 agent 的工具，摊开给人看与试。
 *
 * 页面的立场是「复现 AI 那一侧」——工具集、描述、参数、往返，全部走与
 * agent 完全相同的那条协议路径。所以顶部必须先选**项目**：工具集本身就
 * 随项目的数据源变（没有可用数据源时这个面根本不会挂给 agent），选错
 * 上下文看到的就是另一份工具。
 */
export function Tools() {
  const { t } = useTranslation()
  // 两处「当前选中」都只存**用户显式选过的那个**，实际值靠派生算：
  // 数据到达后用 effect 回填 state 会引发级联渲染，而且工具集随项目变时
  // 还得再写一遍重置逻辑——派生天然没有这两个问题。
  const [pickedProject, setPickedProject] = useState("")
  const [pickedTool, setPickedTool] = useState("")
  // 试运行会产生新的调用记录，跑完把统计重拉一遍。
  const [callVersion, setCallVersion] = useState(0)

  const { data: projects } = useAsyncData(() => api.projects.list(), [])
  const projectItems = projects?.items ?? []
  // 没选过就用最近用过的那个项目：进页面就该看到一份真实的工具集，
  // 而不是一个「请先选择」的空壳。
  //
  // 下拉的值用**项目名**而不是路径：选择器里显示的就是这个值，
  // 一长串绝对路径把控件撑爆了还看不出是哪个项目。
  const project = pickedProject || projectItems[0]?.name || ""
  const cwd = projectItems.find((p) => p.name === project)?.path ?? ""

  const { data: servers, error } = useAsyncData(
    () => api.tools.servers(cwd || undefined),
    [cwd]
  )
  const { data: stats } = useAsyncData(
    () => api.tools.callStats(),
    [callVersion]
  )

  const faces = servers?.items ?? []
  const entries = faces.flatMap((server) =>
    server.tools.map((tool) => ({ server, tool }))
  )
  // 选中的工具不在当前工具集里（换了项目、工具面变了）就退回第一个。
  const selected =
    entries.find((entry) => entry.tool.name === pickedTool) ??
    entries[0] ??
    null
  const unmounted = faces.length > 0 && faces.every((s) => !s.mounted)

  return (
    <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">
      <div className="px-4 lg:px-6">
        <Card>
          <CardHeader>
            <CardTitle>{t("tools.title")}</CardTitle>
            <CardDescription>{t("tools.description")}</CardDescription>
          </CardHeader>

          <CardContent className="flex flex-col gap-4">
            {unmounted ? (
              <Alert>
                <CircleSlashIcon />
                <AlertTitle>{t("tools.unmountedTitle")}</AlertTitle>
                <AlertDescription>{t("tools.unmountedDesc")}</AlertDescription>
              </Alert>
            ) : null}

            <Tabs defaultValue="catalog">
              {/* 页签与上下文选择器同一行：窄屏时自然换行，不去挤页头的
                  说明文字（CardAction 在移动端会把描述压成一列）。 */}
              <div className="flex flex-wrap items-center gap-2">
                <TabsList>
                  <TabsTrigger value="catalog">
                    {t("tools.tabs.catalog")}
                  </TabsTrigger>
                  <TabsTrigger value="calls">
                    {t("tools.tabs.calls")}
                  </TabsTrigger>
                </TabsList>
                <div className="flex-1" />
                <Hint
                  label={t("tools.pickProject")}
                  desc={t("tools.pickProjectHint")}
                >
                  <Select
                    value={project}
                    onValueChange={(v) => setPickedProject(v ?? "")}
                  >
                    <SelectTrigger
                      size="sm"
                      className="w-full sm:w-56"
                      aria-label={t("tools.pickProject")}
                    >
                      <SelectValue placeholder={t("tools.pickProject")} />
                    </SelectTrigger>
                    <SelectContent>
                      {projectItems.map((item) => (
                        <SelectItem key={item.name} value={item.name}>
                          {item.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Hint>
              </div>

              <TabsContent value="catalog" className="mt-4">
                {faces.length === 0 ? (
                  <ListPageStates
                    icon={<WrenchIcon />}
                    error={error}
                    loading={servers === null}
                    emptyTitle={t("tools.emptyTitle")}
                    emptyHint={t("tools.emptyHint")}
                  />
                ) : (
                  <div className="flex flex-col gap-4 md:flex-row md:gap-0">
                    <div className="shrink-0 md:w-72 md:border-r md:pr-3">
                      <ToolCatalog
                        servers={faces}
                        stats={stats?.items ?? []}
                        selected={selected?.tool}
                        onSelect={(_, tool) => setPickedTool(tool.name)}
                      />
                    </div>
                    {selected ? (
                      <ToolDetail
                        // 换工具就换一套参数与结果：靠 key 重建组件，
                        // 比在 effect 里逐个 state 重置可靠得多。
                        key={`${selected.server.name}/${selected.tool.name}`}
                        server={selected.server}
                        tool={selected.tool}
                        cwd={cwd}
                        onCalled={() => setCallVersion((v) => v + 1)}
                      />
                    ) : null}
                  </div>
                )}
              </TabsContent>

              <TabsContent value="calls" className="mt-4">
                <CallLog
                  stats={stats?.items ?? []}
                  onChanged={() => setCallVersion((v) => v + 1)}
                />
              </TabsContent>
            </Tabs>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
