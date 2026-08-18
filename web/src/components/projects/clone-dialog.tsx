import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

import { useAsyncData } from "@/hooks/use-async-data"
import { api } from "@/lib/api"
import type { CloneTask } from "@/types/acp"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { LockIcon, SearchIcon, TriangleAlertIcon } from "lucide-react"

/**
 * 克隆仓库对话框（adr-007）：清单来自 gh，**不含你个人账号名下的仓库**
 * （后端按 affiliation 只要组织与协作者关系）。清单拉不到时不挡路——
 * 手输 URL 这条路始终开着。
 */
export function CloneDialog({
  open,
  onOpenChange,
  onCloned,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCloned: (task: CloneTask) => void
}) {
  const { t } = useTranslation()
  // 只在打开时拉：gh 调用要走网络，关着的对话框不该占它。
  const { data: repos, error } = useAsyncData(
    () =>
      open
        ? api.projects.repos().then((res) => res.items)
        : Promise.resolve([]),
    [open]
  )

  const [query, setQuery] = useState("")
  const [url, setUrl] = useState("")
  const [busy, setBusy] = useState(false)
  const [cloneError, setCloneError] = useState<string | null>(null)

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!repos) return []
    if (!q) return repos.slice(0, 50)
    return repos
      .filter((repo) => repo.name.toLowerCase().includes(q))
      .slice(0, 50)
  }, [repos, query])

  async function clone(cloneUrl: string, name?: string) {
    if (!cloneUrl.trim() || busy) return
    setBusy(true)
    setCloneError(null)
    try {
      const task = await api.projects.clone({ url: cloneUrl.trim(), name })
      onCloned(task)
      onOpenChange(false)
      setQuery("")
      setUrl("")
    } catch (err) {
      setCloneError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("projects.clone")}</DialogTitle>
          <DialogDescription>{t("projects.cloneDesc")}</DialogDescription>
        </DialogHeader>

        <div className="relative">
          <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={query}
            autoFocus
            className="pl-8"
            placeholder={t("projects.searchRepos")}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>

        <ScrollArea className="max-h-72 rounded-lg border border-border">
          {error ? (
            <Alert variant="destructive" className="border-0">
              <TriangleAlertIcon />
              <AlertTitle>{t("projects.reposUnavailable")}</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : repos === null ? (
            <div className="flex flex-col gap-2 p-3">
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-2/3" />
            </div>
          ) : filtered.length === 0 ? (
            <Empty className="py-8">
              <EmptyHeader>
                <EmptyDescription>{t("projects.noRepos")}</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <ul>
              {filtered.map((repo) => (
                <li key={repo.name}>
                  <button
                    type="button"
                    disabled={busy}
                    className="flex w-full flex-col items-start gap-0.5 px-3 py-2 text-left transition-colors duration-150 ease-snappy hover:bg-accent disabled:opacity-50"
                    onClick={() => void clone(repo.cloneUrl, repo.name)}
                  >
                    <span className="flex w-full items-center gap-2">
                      <span className="truncate text-sm font-medium">
                        {repo.name}
                      </span>
                      {repo.private ? (
                        <Badge variant="secondary" className="shrink-0">
                          <LockIcon data-icon="inline-start" />
                          {t("projects.private")}
                        </Badge>
                      ) : null}
                    </span>
                    {repo.description ? (
                      <span className="line-clamp-1 text-xs text-muted-foreground">
                        {repo.description}
                      </span>
                    ) : null}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </ScrollArea>

        <div className="flex flex-col gap-1">
          <Input
            value={url}
            placeholder={t("projects.urlPlaceholder")}
            className="font-mono text-xs"
            onChange={(e) => setUrl(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void clone(url)
            }}
          />
          <p className="text-[11px] text-muted-foreground">
            {t("projects.urlHint")}
          </p>
        </div>
        {cloneError ? (
          <p className="text-xs text-destructive">{cloneError}</p>
        ) : null}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button
            onClick={() => void clone(url)}
            disabled={!url.trim() || busy}
          >
            {t("projects.cloneUrl")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
