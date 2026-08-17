import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { DirPicker } from "@/components/dir-picker"
import { api } from "@/lib/api"
import type { SystemInfo } from "@/types/acp"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { FolderOpenIcon, FolderTreeIcon } from "lucide-react"

/**
 * 工作区根设置：agent 干活的地方，也是局域网访客各自目录的父目录。
 *
 * 与数据目录分成两块刻意为之——数据目录装 db、转录与技能包，把它交给
 * agent 当工作区等于请它往自家数据里写。改这里**立刻生效**，只影响之后
 * 新建的会话与访客，已有的不动（它们的路径已经写进记录了）。
 */
export function WorkspaceDirCard({
  info,
  onChange,
}: {
  info: SystemInfo
  onChange: (info: SystemInfo) => void
}) {
  const { t } = useTranslation()
  const [target, setTarget] = useState("")
  const [pickerOpen, setPickerOpen] = useState(false)
  const [saving, setSaving] = useState(false)

  async function save() {
    const dir = target.trim()
    if (!dir || saving) return
    setSaving(true)
    try {
      onChange(await api.system.setWorkspaceDir(dir))
      setTarget("")
      toast.success(t("settingsPage.workspace.saved"))
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <FolderTreeIcon className="size-4" />
          {t("settingsPage.workspace.title")}
        </CardTitle>
        <CardDescription>
          {t("settingsPage.workspace.description")}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex items-center gap-2">
          <span className="shrink-0 text-sm text-muted-foreground">
            {t("settingsPage.workspace.current")}
          </span>
          <span className="truncate font-mono text-sm">
            {info.workspaceDir}
          </span>
          {info.workspaceDir === info.defaultWorkspaceDir ? (
            <Badge variant="secondary" className="shrink-0">
              {t("settingsPage.system.default")}
            </Badge>
          ) : null}
        </div>

        <div className="flex items-center gap-2">
          <Input
            value={target}
            onChange={(e) => setTarget(e.target.value)}
            placeholder={t("settingsPage.workspace.targetPlaceholder")}
            className="font-mono text-sm"
          />
          <Button
            variant="outline"
            size="sm"
            onClick={() => setPickerOpen(true)}
          >
            <FolderOpenIcon data-icon="inline-start" />
            {t("settingsPage.system.browse")}
          </Button>
          <Button size="sm" disabled={!target.trim() || saving} onClick={save}>
            {t("common.save")}
          </Button>
        </div>
      </CardContent>

      <DirPicker
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        initialPath={target.trim() || info.workspaceDir || undefined}
        onSelect={setTarget}
      />
    </Card>
  )
}
