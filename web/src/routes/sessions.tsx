import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { api } from "@/lib/api"
import { capitalize, formatDateTime, formatRelativeTime } from "@/lib/format"
import type { Session } from "@/types/acp"
import { StatusDot } from "@/components/status-dot"
import { SESSION_STATE_TONE } from "@/lib/status-tone"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
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
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { MessagesSquareIcon, PlusIcon, Trash2Icon } from "lucide-react"

const PAGE_SIZE = 50

export function Sessions() {
  const { t, i18n } = useTranslation()
  const [sessions, setSessions] = useState<Session[] | null>(null)
  const [total, setTotal] = useState(0)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    api.sessions
      .list({ pageSize: PAGE_SIZE })
      .then((res) => {
        setSessions(res.items)
        setTotal(res.total)
      })
      .catch((err: Error) => setError(err.message))
  }, [])

  useEffect(load, [load])

  /** 追加下一页：以已加载条数推算页码，避免维护独立游标。 */
  const loadMore = useCallback(() => {
    const loaded = sessions?.length ?? 0
    api.sessions
      .list({ page: Math.floor(loaded / PAGE_SIZE) + 1, pageSize: PAGE_SIZE })
      .then((res) => {
        setSessions((prev) => {
          const seen = new Set((prev ?? []).map((s) => s.id))
          return [...(prev ?? []), ...res.items.filter((s) => !seen.has(s.id))]
        })
        setTotal(res.total)
      })
      .catch((err: Error) => setError(err.message))
  }, [sessions])

  async function remove(id: number) {
    try {
      await api.sessions.remove(id)
      setSessions((prev) => prev?.filter((s) => s.id !== id) ?? null)
    } catch (err) {
      setError((err as Error).message)
    }
  }

  /** 运行中优先展示（进程活着最值得被看见），出错永远压过其他状态。 */
  function stateLabel(session: Session) {
    if (session.state === "error") {
      return {
        tone: "destructive" as const,
        pulse: false,
        text: t("sessions.stateError"),
      }
    }
    if (session.running) {
      return {
        tone: "success" as const,
        pulse: true,
        text: t("sessions.running"),
      }
    }
    return {
      tone: SESSION_STATE_TONE[session.state],
      pulse: false,
      text: t(`sessions.state${capitalize(session.state)}` as never, {
        defaultValue: session.state,
      }),
    }
  }

  return (
    <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">
      <div className="px-4 lg:px-6">
        <Card>
          <CardHeader>
            <CardTitle>{t("sessions.title")}</CardTitle>
            <CardDescription>{t("sessions.description")}</CardDescription>
            <CardAction>
              <Button size="sm" render={<Link to="/sessions/new" />}>
                <PlusIcon data-icon="inline-start" />
                {t("sessions.create")}
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            {error ? (
              <Empty>
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <MessagesSquareIcon />
                  </EmptyMedia>
                  <EmptyTitle>{t("common.loadFailed")}</EmptyTitle>
                  <EmptyDescription>{error}</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : sessions === null ? (
              <div className="flex flex-col gap-2">
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
              </div>
            ) : sessions.length === 0 ? (
              <Empty>
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <MessagesSquareIcon />
                  </EmptyMedia>
                  <EmptyTitle>{t("sessions.empty")}</EmptyTitle>
                  <EmptyDescription>{t("sessions.emptyHint")}</EmptyDescription>
                </EmptyHeader>
                <EmptyContent>
                  <Button size="sm" render={<Link to="/sessions/new" />}>
                    <PlusIcon data-icon="inline-start" />
                    {t("sessions.create")}
                  </Button>
                </EmptyContent>
              </Empty>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("sessions.columnTitle")}</TableHead>
                    <TableHead>{t("sessions.agent")}</TableHead>
                    <TableHead className="text-right">
                      {t("sessions.messages")}
                    </TableHead>
                    <TableHead>{t("sessions.state")}</TableHead>
                    <TableHead>{t("sessions.updated")}</TableHead>
                    <TableHead className="w-10" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {sessions.map((session) => {
                    const state = stateLabel(session)
                    return (
                      <TableRow key={session.id} className="group relative">
                        <TableCell className="max-w-64 font-medium">
                          {/* 拉伸链接铺满整行：视觉上整行可点，语义仍是 <a>。 */}
                          <Link
                            to={`/sessions/${session.id}`}
                            className="truncate after:absolute after:inset-0"
                          >
                            {session.title ||
                              `${t("common.unnamed")} #${session.id}`}
                          </Link>
                        </TableCell>
                        <TableCell className="text-muted-foreground">
                          {session.agentName}
                        </TableCell>
                        <TableCell className="text-right text-muted-foreground tabular-nums">
                          {session.messageCount}
                        </TableCell>
                        <TableCell>
                          <StatusDot
                            tone={state.tone}
                            pulse={state.pulse}
                            label={state.text}
                          />
                        </TableCell>
                        <TableCell
                          className="text-muted-foreground tabular-nums"
                          title={formatDateTime(
                            session.updatedAt,
                            i18n.language
                          )}
                        >
                          {formatRelativeTime(session.updatedAt, i18n.language)}
                        </TableCell>
                        <TableCell className="py-0">
                          <DeleteSessionButton
                            onConfirm={() => void remove(session.id)}
                          />
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            )}
            {sessions !== null && sessions.length < total ? (
              <div className="flex justify-center py-3">
                <Button
                  variant="ghost"
                  size="sm"
                  className="text-muted-foreground"
                  onClick={loadMore}
                >
                  {t("sessions.loadMore", {
                    loaded: sessions.length,
                    total,
                  })}
                </Button>
              </div>
            ) : null}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

/** 删除按钮：默认隐身，行 hover / 聚焦时浮现；确认走 AlertDialog 而非原生 confirm。 */
function DeleteSessionButton({ onConfirm }: { onConfirm: () => void }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  return (
    <AlertDialog open={open} onOpenChange={setOpen}>
      <AlertDialogTrigger
        render={
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t("common.delete")}
            className="relative text-muted-foreground opacity-0 transition-opacity duration-150 group-hover:opacity-100 hover:text-destructive focus-visible:opacity-100 aria-expanded:opacity-100"
          />
        }
      >
        <Trash2Icon />
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("sessions.deleteTitle")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("sessions.deleteConfirm")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            onClick={() => {
              setOpen(false)
              onConfirm()
            }}
          >
            {t("common.delete")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

