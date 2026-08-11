import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { api } from "@/lib/api"
import type { Session, SessionState } from "@/types/acp"
import { Badge } from "@/components/ui/badge"
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

const STATE_VARIANT: Record<
  SessionState,
  "default" | "secondary" | "outline" | "destructive"
> = {
  active: "default",
  idle: "secondary",
  ended: "outline",
  error: "destructive",
}

export function Sessions() {
  const { t } = useTranslation()
  const [sessions, setSessions] = useState<Session[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    api.sessions
      .list()
      .then((res) => setSessions(res.items))
      .catch((err: Error) => setError(err.message))
  }, [])

  useEffect(load, [load])

  async function remove(id: number) {
    if (!window.confirm(t("sessions.deleteConfirm"))) return
    try {
      await api.sessions.remove(id)
      setSessions((prev) => prev?.filter((s) => s.id !== id) ?? null)
    } catch (err) {
      setError((err as Error).message)
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
                <Skeleton className="h-9 w-full" />
                <Skeleton className="h-9 w-full" />
                <Skeleton className="h-9 w-full" />
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
              </Empty>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("sessions.columnTitle")}</TableHead>
                    <TableHead>{t("sessions.agent")}</TableHead>
                    <TableHead>{t("sessions.messages")}</TableHead>
                    <TableHead>{t("sessions.state")}</TableHead>
                    <TableHead>{t("sessions.updated")}</TableHead>
                    <TableHead className="w-10" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {sessions.map((session) => (
                    <TableRow key={session.id}>
                      <TableCell className="font-medium">
                        <Link
                          to={`/sessions/${session.id}`}
                          className="hover:underline"
                        >
                          {session.title ||
                            `${t("common.unnamed")} #${session.id}`}
                        </Link>
                      </TableCell>
                      <TableCell>{session.agentName}</TableCell>
                      <TableCell>{session.messageCount}</TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Badge variant={STATE_VARIANT[session.state]}>
                            {t(
                              `sessions.state${capitalize(session.state)}` as never,
                              {
                                defaultValue: session.state,
                              }
                            )}
                          </Badge>
                          {session.running ? (
                            <Badge variant="secondary">
                              {t("sessions.running")}
                            </Badge>
                          ) : null}
                        </div>
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {new Date(session.updatedAt).toLocaleString()}
                      </TableCell>
                      <TableCell>
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={t("common.delete")}
                          onClick={() => void remove(session.id)}
                        >
                          <Trash2Icon />
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function capitalize(value: string) {
  return value.charAt(0).toUpperCase() + value.slice(1)
}
