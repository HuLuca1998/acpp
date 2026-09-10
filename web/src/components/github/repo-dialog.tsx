import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { useAsyncData } from "@/hooks/use-async-data"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Spinner } from "@/components/ui/spinner"
import { LockIcon } from "lucide-react"

/**
 * 关注仓库对话框：gh 登录账号能看到的全部仓库列成勾选清单，保存即覆盖
 * 当前身份的关注清单。清单可能有几十上百条，顶部给一个过滤框。
 */
export function RepoDialog({
  open,
  onOpenChange,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 保存成功后回调新的关注清单，页面据此重拉 issue。 */
  onSaved: (repos: string[]) => void
}) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("github.watchRepos")}</DialogTitle>
          <DialogDescription>{t("github.watchReposDesc")}</DialogDescription>
        </DialogHeader>
        {/* 内容随对话框一起卸载：每次打开都是全新一份，清单与勾选态都重取。 */}
        <RepoPicker
          onCancel={() => onOpenChange(false)}
          onSaved={(repos) => {
            onSaved(repos)
            onOpenChange(false)
          }}
        />
      </DialogContent>
    </Dialog>
  )
}

function RepoPicker({
  onCancel,
  onSaved,
}: {
  onCancel: () => void
  onSaved: (repos: string[]) => void
}) {
  const { t } = useTranslation()
  const { data: repos, error } = useAsyncData(() => api.github.repos(), [])
  // 勾选态在用户第一次动手之前跟着清单走（库里已关注的），动过之后才由
  // 本地状态接管——这样清单到达时不需要一个 effect 去同步。
  const [picked, setPicked] = useState<Set<string> | null>(null)
  const selected = useMemo(
    () =>
      picked ??
      new Set((repos ?? []).filter((r) => r.watched).map((r) => r.name)),
    [picked, repos]
  )
  const [filter, setFilter] = useState("")
  const [saving, setSaving] = useState(false)

  // 已关注的排前面：清单几十条，用户最常做的是「看看现在关注了哪些、
  // 去掉一个」，不该让他在字母序里找。
  const visible = useMemo(() => {
    const q = filter.trim().toLowerCase()
    if (!repos) return []
    const list = q
      ? repos.filter((r) => r.name.toLowerCase().includes(q))
      : repos
    return [...list].sort(
      (a, b) =>
        Number(b.watched) - Number(a.watched) || a.name.localeCompare(b.name)
    )
  }, [repos, filter])

  function toggle(name: string, on: boolean) {
    const next = new Set(selected)
    if (on) next.add(name)
    else next.delete(name)
    setPicked(next)
  }

  async function save() {
    setSaving(true)
    try {
      onSaved(await api.github.setRepos([...selected]))
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <Input
        value={filter}
        placeholder={t("github.filterRepos")}
        onChange={(e) => setFilter(e.target.value)}
      />
      <ScrollArea className="h-80 rounded-lg border">
        {error ? (
          <p className="p-4 text-sm text-destructive">{error}</p>
        ) : repos === null ? (
          <div className="flex h-full items-center justify-center p-4">
            <Spinner />
          </div>
        ) : visible.length === 0 ? (
          <p className="p-4 text-sm text-muted-foreground">
            {t("github.noRepos")}
          </p>
        ) : (
          <ul className="flex flex-col p-1">
            {visible.map((repo) => (
              <li key={repo.name}>
                <label
                  htmlFor={`repo-${repo.name}`}
                  className="flex cursor-default items-center gap-2.5 rounded-md px-2 py-1.5 text-sm hover:bg-muted/60"
                >
                  <Checkbox
                    id={`repo-${repo.name}`}
                    checked={selected.has(repo.name)}
                    onCheckedChange={(on) => toggle(repo.name, on === true)}
                  />
                  <span className="flex-1 truncate font-mono text-xs">
                    {repo.name}
                  </span>
                  {repo.private ? (
                    <LockIcon className="size-3 text-muted-foreground" />
                  ) : null}
                </label>
              </li>
            ))}
          </ul>
        )}
      </ScrollArea>
      <DialogFooter>
        <span className="mr-auto self-center text-xs text-muted-foreground tabular-nums">
          {t("github.selectedCount", { count: selected.size })}
        </span>
        <Button variant="outline" onClick={onCancel}>
          {t("common.cancel")}
        </Button>
        <Button onClick={() => void save()} disabled={saving || !repos}>
          {t("common.save")}
        </Button>
      </DialogFooter>
    </>
  )
}
