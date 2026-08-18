import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"
import { toast } from "sonner"

import { ListPageStates } from "@/components/list-page-states"
import { StatusDot } from "@/components/status-dot"
import { usePagedData } from "@/hooks/use-paged-data"
import { DataPagination } from "@/components/data-pagination"
import { api } from "@/lib/api"
import { formatDateTime, formatRelativeTime } from "@/lib/format"
import { SESSION_STATE_TONE } from "@/lib/status-tone"
import type { OrchSession } from "@/types/acp"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { NetworkIcon, PlusIcon, Trash2Icon } from "lucide-react"

/** 编排会话列表（adr-006）。新建走 draft-first：直接进空白编排页。 */
export function Orchestrator() {
  const { t, i18n } = useTranslation()
  const {
    items: sessions,
    total,
    error,
    page,
    pageSize,
    setPage,
    setPageSize,
    remove: dropRow,
  } = usePagedData((params) => api.orchestrator.list(params))
  const [deleting, setDeleting] = useState<OrchSession | null>(null)

  async function remove() {
    if (!deleting) return
    try {
      await api.orchestrator.remove(deleting.id)
      dropRow(deleting.id)
      toast.success(t("orch.deleted"))
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setDeleting(null)
    }
  }

  return (
    <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">
      <div className="px-4 lg:px-6">
        <Card>
          <CardHeader>
            <CardTitle>{t("orch.title")}</CardTitle>
            <CardDescription>{t("orch.description")}</CardDescription>
            <CardAction>
              <Button size="sm" render={<Link to="/orchestrator/new" />}>
                <PlusIcon data-icon="inline-start" />
                {t("orch.add")}
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            {error || sessions === null || sessions.length === 0 ? (
              <ListPageStates
                icon={<NetworkIcon />}
                error={error}
                loading={sessions === null}
                emptyTitle={t("orch.empty")}
                emptyHint={t("orch.emptyListHint")}
                emptyAction={
                  <Button size="sm" render={<Link to="/orchestrator/new" />}>
                    <PlusIcon data-icon="inline-start" />
                    {t("orch.add")}
                  </Button>
                }
              />
            ) : (
              <>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t("orch.colTitle")}</TableHead>
                      <TableHead>{t("orch.colCwd")}</TableHead>
                      <TableHead className="text-right">
                        {t("orch.colTokens")}
                      </TableHead>
                      <TableHead className="text-right">
                        {t("orch.colUpdated")}
                      </TableHead>
                      <TableHead className="w-10" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {sessions.map((session) => (
                      <TableRow key={session.id} className="group relative">
                        <TableCell className="font-medium">
                          <span className="inline-flex items-center gap-2">
                            <StatusDot
                              tone={SESSION_STATE_TONE[session.state]}
                              pulse={session.state === "active"}
                            />
                            <Link
                              to={`/orchestrator/${session.id}`}
                              className="after:absolute after:inset-0"
                            >
                              {session.title ||
                                `${t("common.unnamed")} #${session.id}`}
                            </Link>
                          </span>
                        </TableCell>
                        <TableCell className="max-w-72 truncate font-mono text-xs text-muted-foreground">
                          {session.cwd || t("common.none")}
                        </TableCell>
                        <TableCell className="text-right text-muted-foreground tabular-nums">
                          {session.tokensUsed > 0
                            ? session.tokensUsed.toLocaleString()
                            : t("common.none")}
                        </TableCell>
                        <TableCell
                          className="text-right text-muted-foreground tabular-nums"
                          title={formatDateTime(
                            session.updatedAt,
                            i18n.language
                          )}
                        >
                          {formatRelativeTime(session.updatedAt, i18n.language)}
                        </TableCell>
                        <TableCell className="text-right">
                          <Button
                            size="icon-sm"
                            variant="ghost"
                            className="relative text-muted-foreground opacity-0 group-hover:opacity-100 hover:text-destructive focus-visible:opacity-100"
                            aria-label={t("common.delete")}
                            onClick={() => setDeleting(session)}
                          >
                            <Trash2Icon />
                          </Button>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
                <DataPagination
                  total={total}
                  page={page}
                  pageSize={pageSize}
                  onPage={setPage}
                  onPageSize={setPageSize}
                />
              </>
            )}
          </CardContent>
        </Card>
      </div>

      <AlertDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("orch.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("orch.deleteBody", {
                name: deleting?.title || `#${deleting?.id ?? ""}`,
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={remove}>
              {t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
