import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { CloneDialog } from "@/components/projects/clone-dialog"
import { ProjectCards } from "@/components/projects/project-cards"
import { useAsyncData } from "@/hooks/use-async-data"
import { api } from "@/lib/api"
import type { CloneTask, Project } from "@/types/acp"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"

/** 克隆进行中的轮询间隔：git clone 是分钟级的事，秒级轮询没有意义。 */
const CLONE_POLL_MS = 3000

/**
 * 项目条（adr-007）：会话页顶部的项目入口——克隆仓库、开空项目、
 * 点进项目直接以它为工作目录建会话。
 *
 * 租户的导航里只有会话页，所以项目管理必须落在这里，不能另开一个菜单。
 */
export function ProjectsBar() {
  const { t } = useTranslation()
  // 项目区拉不到就空着：会话列表本身与它无关，不该跟着报错。
  const [reloadKey, setReloadKey] = useState(0)
  const { data: projects } = useAsyncData<Project[]>(
    () =>
      api.projects
        .list()
        .then((res) => res.items)
        .catch(() => []),
    [reloadKey]
  )
  const reload = useCallback(() => setReloadKey((key) => key + 1), [])

  const [clones, setClones] = useState<CloneTask[]>([])
  const [cloneOpen, setCloneOpen] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [newName, setNewName] = useState("")
  const [createError, setCreateError] = useState<string | null>(null)

  // 只在有任务在跑时轮询，跑完立刻刷新项目列表让新仓库出现。
  useEffect(() => {
    const running = clones.some((clone) => clone.state === "running")
    if (!running) return

    const timer = setInterval(() => {
      api.projects
        .clones()
        .then((res) => {
          const active = res.items.filter((clone) => clone.state === "running")
          const failed = res.items.filter(
            (clone) =>
              clone.state === "failed" &&
              clones.some(
                (prev) => prev.id === clone.id && prev.state === "running"
              )
          )
          for (const clone of failed) {
            toast.error(clone.error || t("projects.cloneFailed"))
          }
          if (active.length !== clones.length) reload()
          setClones(active)
        })
        .catch(() => {
          // 轮询失败下一轮再来，不打扰用户。
        })
    }, CLONE_POLL_MS)
    return () => clearInterval(timer)
  }, [clones, reload, t])

  async function create() {
    const name = newName.trim()
    if (!name) return
    try {
      await api.projects.create(name)
      setCreateOpen(false)
      setNewName("")
      setCreateError(null)
      reload()
    } catch (err) {
      setCreateError((err as Error).message)
    }
  }

  return (
    <>
      <ProjectCards
        projects={projects ?? []}
        clones={clones}
        onClone={() => setCloneOpen(true)}
        onCreate={() => setCreateOpen(true)}
      />

      <CloneDialog
        open={cloneOpen}
        onOpenChange={setCloneOpen}
        onCloned={(task) => setClones((prev) => [...prev, task])}
      />

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("projects.create")}</DialogTitle>
            <DialogDescription>{t("projects.createDesc")}</DialogDescription>
          </DialogHeader>
          <Field>
            <FieldLabel htmlFor="project-name">{t("projects.name")}</FieldLabel>
            <Input
              id="project-name"
              value={newName}
              autoFocus
              placeholder={t("projects.namePlaceholder")}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") void create()
              }}
            />
            <FieldDescription>
              {createError ?? t("projects.nameHint")}
            </FieldDescription>
          </Field>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button onClick={() => void create()} disabled={!newName.trim()}>
              {t("projects.create")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
