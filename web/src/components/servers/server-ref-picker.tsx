import { useTranslation } from "react-i18next"

import { api } from "@/lib/api"
import { useAsyncData } from "@/hooks/use-async-data"
import type { Server } from "@/types/acp"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { HardDriveIcon } from "lucide-react"

/**
 * 选一台服务器交给 AI，与 @ 数据库引用是同一个动作。
 *
 * 只有一级——引用的粒度就是**机器**，不带路径（adr-019）。要看哪个目录
 * 由 AI 从项目代码推断，那是这套东西的主线；把路径也塞进引用反而会让它
 * 省掉读代码那一步，路径过时了还无从发现。
 */
export function ServerRefPicker({
  open,
  onOpenChange,
  onSelect,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSelect: (name: string) => void
}) {
  const { t } = useTranslation()
  // 每次打开重拉：服务器页刚加的那台，下一次引用就该看得见。
  const { data, error } = useAsyncData(
    () =>
      open
        ? api.workspaceServers().then((r) => r.items)
        : Promise.resolve([] as Server[]),
    [open]
  )

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("server.refTitle")}</DialogTitle>
          <DialogDescription>{t("server.refHint")}</DialogDescription>
        </DialogHeader>

        <ScrollArea className="max-h-80">
          <div className="flex flex-col gap-1 pr-3">
            {error ? (
              <p className="p-2 text-sm text-destructive">{error}</p>
            ) : data === null ? (
              <>
                <Skeleton className="h-12 w-full" />
                <Skeleton className="h-12 w-full" />
              </>
            ) : data.length === 0 ? (
              <p className="p-2 text-sm text-muted-foreground">
                {t("server.empty")}
              </p>
            ) : (
              data.map((srv) => (
                <button
                  key={srv.id}
                  type="button"
                  className="flex items-start gap-2 rounded-md px-2 py-2 text-left hover:bg-accent"
                  onClick={() => {
                    onSelect(srv.name)
                    onOpenChange(false)
                  }}
                >
                  <HardDriveIcon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1">
                    <span className="block font-mono text-sm">{srv.name}</span>
                    <span className="block truncate text-xs text-muted-foreground">
                      {srv.note || `${srv.user}@${srv.host}:${srv.port}`}
                    </span>
                  </span>
                </button>
              ))
            )}
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}
