import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import type { CloneTask, Project } from "@/types/acp"
import {
  DownloadCloudIcon,
  FolderGitIcon,
  GitBranchIcon,
  PlusIcon,
} from "lucide-react"

/**
 * 项目卡片带（adr-007）：工作区里的仓库，点进去就以它为工作目录开会话。
 * 进行中的克隆任务混在同一条带子里，位置不跳。
 */
export function ProjectCards({
  projects,
  clones,
  onClone,
  onCreate,
}: {
  projects: Project[]
  clones: CloneTask[]
  onClone: () => void
  onCreate: () => void
}) {
  const { t } = useTranslation()

  return (
    <div className="flex flex-wrap gap-2">
      {clones.map((clone) => (
        <div
          key={clone.id}
          className="flex min-w-56 items-center gap-2 rounded-lg border border-dashed border-border bg-card/50 px-3 py-2"
        >
          <Spinner className="size-4 shrink-0 text-muted-foreground" />
          <div className="min-w-0">
            <div className="truncate text-sm font-medium">{clone.name}</div>
            <div className="truncate text-xs text-muted-foreground">
              {t("projects.cloning")}
            </div>
          </div>
        </div>
      ))}

      {projects.map((project) => (
        <Link
          key={project.name}
          to={`/sessions/new?cwd=${encodeURIComponent(project.path)}`}
          className="group flex min-w-56 flex-col gap-1 rounded-lg border border-border bg-card px-3 py-2 transition-colors duration-150 ease-snappy hover:bg-accent"
        >
          <div className="flex items-center gap-2">
            <FolderGitIcon className="size-4 shrink-0 text-muted-foreground" />
            <span className="truncate text-sm font-medium" title={project.name}>
              {project.name}
            </span>
          </div>
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            {project.branch ? (
              <span className="inline-flex min-w-0 items-center gap-1">
                <GitBranchIcon className="size-3 shrink-0" />
                <span className="truncate font-mono">{project.branch}</span>
              </span>
            ) : null}
            <Badge
              variant="secondary"
              title={t("projects.sessions")}
              className="ml-auto tabular-nums"
            >
              {project.sessionCount}
            </Badge>
          </div>
        </Link>
      ))}

      <div className="flex items-center gap-2">
        <Button size="sm" variant="outline" onClick={onClone}>
          <DownloadCloudIcon data-icon="inline-start" />
          {t("projects.clone")}
        </Button>
        <Button size="sm" variant="ghost" onClick={onCreate}>
          <PlusIcon data-icon="inline-start" />
          {t("projects.create")}
        </Button>
      </div>
    </div>
  )
}
