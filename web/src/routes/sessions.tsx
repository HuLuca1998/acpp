import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"
import type { ColumnDef } from "@tanstack/react-table"

import { Hint } from "@/components/hint"
import { ListPageStates } from "@/components/list-page-states"
import { usePagedData } from "@/hooks/use-paged-data"
import { DataTable } from "@/components/data-table/data-table"
import { DataTableHeader } from "@/components/data-table/data-table-header"
import type { dataTableFeatures } from "@/components/data-table/data-table-features"
import { useIdentity } from "@/hooks/identity-context"
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
import { MessagesSquareIcon, PlusIcon, Trash2Icon } from "lucide-react"

/** 列定义的类型别名：v9 的第一个泛型是 features 不是 data，写全太吵。 */
type SessionColumn = ColumnDef<typeof dataTableFeatures, Session, unknown>

export function Sessions() {
  const { t, i18n } = useTranslation()
  // 创建者列只对 owner 有意义：租户只看得见自己的会话，那一列对他恒为
  // 自己，白占一列宽度（adr-007 的隔离已经保证了这一点）。
  const isOwner = useIdentity().identity?.owner ?? false
  const {
    items: sessions,
    total,
    error,
    page,
    pageSize,
    sorting,
    setPage,
    setPageSize,
    setSorting,
    remove: dropRow,
    setError,
  } = usePagedData((params) => api.sessions.list(params))
  async function remove(id: number) {
    try {
      await api.sessions.remove(id)
      dropRow(id)
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

  // 出错时不留半张旧表：整个让给三态壳去说明。
  const rows = error ? null : sessions

  // 列 id 就是**数据库列名**：它要原样进 `?sort=`，再由后端的白名单校验。
  // 多一层「前端列名 → 数据库列名」的映射表，只会多一个能写错的地方。
  const columns: SessionColumn[] = [
    {
      id: "title",
      // accessorFn 只是让这列成为 accessor 列（display 列不可排序），
      // 真正的排序由后端做（manualSorting）。
      accessorFn: (session: Session) => session.title,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("sessions.columnTitle")} />
      ),
      // max-w 与 block 缺一不可：Link 默认是 inline，truncate 在 inline 上
      // 完全不生效，长标题会撑破单元格盖住右边的列。
      meta: { label: t("sessions.columnTitle"), className: "max-w-64" },
      cell: ({ row }) => (
        // 拉伸链接铺满整行：视觉上整行可点，语义仍是 <a>。
        <Link
          to={`/sessions/${row.original.id}`}
          className="block truncate font-medium after:absolute after:inset-0"
        >
          {row.original.title || `${t("common.unnamed")} #${row.original.id}`}
        </Link>
      ),
    },
    {
      id: "agent_id",
      accessorFn: (session: Session) => session.agentName,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("sessions.agent")} />
      ),
      meta: { label: t("sessions.agent"), className: "text-muted-foreground" },
      cell: ({ row }) => row.original.agentName,
    },
    // 创建者列只对 owner 有意义：租户只看得见自己的会话，那一列对他恒为
    // 自己，白占一列宽度（adr-007 的隔离已经保证了这一点）。
    ...(isOwner
      ? ([
          {
            id: "tenant_id",
            accessorFn: (session: Session) => session.tenantName,
            header: ({ column }) => (
              <DataTableHeader column={column} title={t("sessions.creator")} />
            ),
            meta: {
              label: t("sessions.creator"),
              className: "text-muted-foreground",
            },
            // 空的 tenantName 就是 owner 自己——他不在租户表里，没有记录
            // 可查（adr-007）。
            cell: ({ row }) =>
              row.original.tenantName
                ? capitalize(row.original.tenantName)
                : t("identity.admin"),
          },
        ] satisfies SessionColumn[])
      : []),
    {
      id: "message_count",
      accessorFn: (session: Session) => session.messageCount,
      header: ({ column }) => (
        <DataTableHeader
          column={column}
          title={t("sessions.messages")}
          className="ml-auto"
        />
      ),
      meta: {
        label: t("sessions.messages"),
        className: "text-right text-muted-foreground tabular-nums",
      },
      cell: ({ row }) => row.original.messageCount,
    },
    {
      id: "state",
      accessorFn: (session: Session) => session.state,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("sessions.state")} />
      ),
      meta: { label: t("sessions.state") },
      cell: ({ row }) => {
        const state = stateLabel(row.original)
        return (
          <StatusDot tone={state.tone} pulse={state.pulse} label={state.text} />
        )
      },
    },
    {
      id: "updated_at",
      accessorFn: (session: Session) => session.updatedAt,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("sessions.updated")} />
      ),
      meta: {
        label: t("sessions.updated"),
        className: "text-muted-foreground tabular-nums",
      },
      cell: ({ row }) => (
        <span title={formatDateTime(row.original.updatedAt, i18n.language)}>
          {formatRelativeTime(row.original.updatedAt, i18n.language)}
        </span>
      ),
    },
    {
      id: "actions",
      enableSorting: false,
      enableHiding: false,
      meta: { className: "w-10 py-0" },
      cell: ({ row }) => (
        <DeleteSessionButton onConfirm={() => void remove(row.original.id)} />
      ),
    },
  ]

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
            <DataTable
              columns={columns}
              data={rows}
              total={total}
              page={page}
              pageSize={pageSize}
              sorting={sorting}
              onPage={setPage}
              onPageSize={setPageSize}
              onSorting={setSorting}
              empty={
                <ListPageStates
                  icon={<MessagesSquareIcon />}
                  error={error}
                  loading={sessions === null}
                  emptyTitle={t("sessions.empty")}
                  emptyHint={t("sessions.emptyHint")}
                  emptyAction={
                    <Button size="sm" render={<Link to="/sessions/new" />}>
                      <PlusIcon data-icon="inline-start" />
                      {t("sessions.create")}
                    </Button>
                  }
                />
              }
            />
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
      <Hint
        label={t("sessions.deleteTitle")}
        desc={t("sessions.deleteHint")}
        align="end"
      >
        <AlertDialogTrigger
          render={
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={t("common.delete")}
              className="relative text-muted-foreground transition-colors hover:text-destructive"
            >
              <Trash2Icon />
            </Button>
          }
        />
      </Hint>
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
