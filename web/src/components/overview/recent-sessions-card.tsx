import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { StatusDot } from "@/components/status-dot"
import { SESSION_STATE_TONE } from "@/lib/status-tone"
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
import { formatDateTime, formatRelativeTime } from "@/lib/format"
import type { Session } from "@/types/acp"
import { ArrowRightIcon, MessagesSquareIcon, PlusIcon } from "lucide-react"

/** 最近会话卡：整行可点进入会话，空态引导新建。 */
export function RecentSessionsCard({ sessions }: { sessions: Session[] }) {
  const { t, i18n } = useTranslation()

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("overview.recentTitle")}</CardTitle>
        <CardDescription>{t("overview.recentDescription")}</CardDescription>
        <CardAction>
          <Button size="sm" variant="ghost" render={<Link to="/sessions" />}>
            {t("nav.viewAll")}
            <ArrowRightIcon data-icon="inline-end" />
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        {sessions.length === 0 ? (
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <MessagesSquareIcon />
              </EmptyMedia>
              <EmptyTitle>{t("overview.noSessions")}</EmptyTitle>
              <EmptyDescription>{t("overview.noSessionsHint")}</EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button size="sm" render={<Link to="/sessions/new" />}>
                <PlusIcon data-icon="inline-start" />
                {t("overview.newSession")}
              </Button>
            </EmptyContent>
          </Empty>
        ) : (
          <div className="-mx-2 flex flex-col">
            {sessions.map((session) => (
              <Link
                key={session.id}
                to={`/sessions/${session.id}`}
                className="group flex items-center gap-3 rounded-lg px-2 py-2.5 transition-colors duration-150 ease-snappy hover:bg-muted/60"
              >
                <StatusDot
                  tone={
                    session.running
                      ? "success"
                      : SESSION_STATE_TONE[session.state]
                  }
                  pulse={session.running}
                />
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate text-sm font-medium">
                    {session.title || `${t("common.unnamed")} #${session.id}`}
                  </span>
                  <span className="truncate text-xs text-muted-foreground">
                    {session.agentName} ·{" "}
                    {t("overview.messagesCount", {
                      count: session.messageCount,
                    })}
                  </span>
                </span>
                <span
                  className="shrink-0 text-xs text-muted-foreground tabular-nums"
                  title={formatDateTime(session.updatedAt, i18n.language)}
                >
                  {formatRelativeTime(session.updatedAt, i18n.language)}
                </span>
                <ArrowRightIcon className="size-3.5 shrink-0 -translate-x-1 text-muted-foreground opacity-0 transition-[opacity,translate] duration-150 ease-snappy group-hover:translate-x-0 group-hover:opacity-100" />
              </Link>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
