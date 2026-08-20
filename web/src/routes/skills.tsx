import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"
import { toast } from "sonner"

import { Hint } from "@/components/hint"
import { ListPageStates } from "@/components/list-page-states"
import { usePagedData } from "@/hooks/use-paged-data"
import { DataTable } from "@/components/data-table/data-table"
import { DataTableHeader } from "@/components/data-table/data-table-header"
import type { dataTableFeatures } from "@/components/data-table/data-table-features"
import type { ColumnDef } from "@tanstack/react-table"

import { api } from "@/lib/api"
import { formatDateTime, formatRelativeTime } from "@/lib/format"
import type { Skill } from "@/types/acp"

type SkillColumn = ColumnDef<typeof dataTableFeatures, Skill, unknown>
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
import { Switch } from "@/components/ui/switch"
import { InfoIcon, PlusIcon, PuzzleIcon, Trash2Icon } from "lucide-react"

export function Skills() {
  const { t, i18n } = useTranslation()
  const {
    items: skills,
    total,
    error,
    page,
    pageSize,
    sorting,
    setPage,
    setPageSize,
    setSorting,
    patch,
    remove: dropRow,
  } = usePagedData((params) => api.skills.list(params), {
    keyOf: (s) => s.name,
  })
  const [deleting, setDeleting] = useState<Skill | null>(null)

  async function toggle(skill: Skill, enabled: boolean) {
    // 乐观更新：符号链接切换基本不会失败，失败时回滚并提示。
    patch(skill.name, (s) => ({ ...s, enabled }))
    try {
      await api.skills.update(skill.name, { enabled })
      toast.success(
        t(enabled ? "skills.enabledToast" : "skills.disabledToast", {
          name: skill.name,
        }),
        { description: t("skills.effectNote") }
      )
    } catch (err) {
      patch(skill.name, (s) => ({ ...s, enabled: !enabled }))
      toast.error((err as Error).message)
    }
  }

  async function remove() {
    if (!deleting) return
    try {
      await api.skills.remove(deleting.name)
      dropRow(deleting.name)
      toast.success(t("skills.deleted"))
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setDeleting(null)
    }
  }

  // 技能没有数据库，排序在后端的内存里做，但字段名与其他列表端点是同一套
  // 写法——前端不必知道哪个端点背后是 SQL、哪个是磁盘。
  const columns: SkillColumn[] = [
    {
      id: "name",
      accessorFn: (skill: Skill) => skill.name,
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("skills.name")} />
      ),
      meta: {
        label: t("skills.name"),
        className: "font-mono text-xs font-medium",
      },
      cell: ({ row }) => (
        // 拉伸链接铺满整行；开关与删除在链接之上单独可点。
        <Link
          to={`/skills/${row.original.name}`}
          className="after:absolute after:inset-0"
        >
          {row.original.name}
        </Link>
      ),
    },
    {
      id: "description",
      enableSorting: false,
      header: t("skills.colDescription"),
      meta: {
        label: t("skills.colDescription"),
        className: "max-w-96 truncate text-muted-foreground",
      },
      cell: ({ row }) => row.original.description || t("common.none"),
    },
    {
      id: "usage_count",
      accessorFn: (skill: Skill) => skill.usageCount,
      header: ({ column }) => (
        <DataTableHeader
          column={column}
          title={t("skills.usage")}
          className="ml-auto"
        />
      ),
      meta: { label: t("skills.usage"), className: "text-right tabular-nums" },
      cell: ({ row }) =>
        row.original.usageCount > 0 ? (
          row.original.usageCount.toLocaleString()
        ) : (
          <span className="text-muted-foreground">{t("common.none")}</span>
        ),
    },
    {
      id: "updated_at",
      accessorFn: (skill: Skill) => skill.updatedAt,
      header: ({ column }) => (
        <DataTableHeader
          column={column}
          title={t("skills.updated")}
          className="ml-auto"
        />
      ),
      meta: {
        label: t("skills.updated"),
        className: "text-right text-muted-foreground tabular-nums",
      },
      cell: ({ row }) => (
        <span title={formatDateTime(row.original.updatedAt, i18n.language)}>
          {formatRelativeTime(row.original.updatedAt, i18n.language)}
        </span>
      ),
    },
    {
      id: "enabled",
      accessorFn: (skill: Skill) => skill.enabled,
      header: ({ column }) => (
        <DataTableHeader
          column={column}
          title={t("skills.enabled")}
          className="ml-auto"
        />
      ),
      meta: { label: t("skills.enabled"), className: "w-24 text-right" },
      cell: ({ row }) => (
        <Switch
          className="relative"
          checked={row.original.enabled}
          onCheckedChange={(on) => toggle(row.original, on)}
          aria-label={t("skills.enabled")}
        />
      ),
    },
    {
      id: "actions",
      enableSorting: false,
      enableHiding: false,
      meta: { className: "w-10 text-right" },
      cell: ({ row }) => (
        <Hint label={t("skills.deleteTitle")} align="end">
          <Button
            size="icon-sm"
            variant="ghost"
            className="relative text-muted-foreground opacity-0 group-hover:opacity-100 hover:text-destructive focus-visible:opacity-100"
            aria-label={t("common.delete")}
            onClick={() => setDeleting(row.original)}
          >
            <Trash2Icon />
          </Button>
        </Hint>
      ),
    },
  ]

  return (
    <div className="flex flex-col gap-4 py-4 md:gap-6 md:py-6">
      <div className="px-4 lg:px-6">
        <Card>
          <CardHeader>
            <CardTitle>{t("skills.title")}</CardTitle>
            <CardDescription>{t("skills.description")}</CardDescription>
            <CardAction>
              <Button size="sm" render={<Link to="/skills/new" />}>
                <PlusIcon data-icon="inline-start" />
                {t("skills.add")}
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            <DataTable
              columns={columns}
              data={error ? null : skills}
              total={total}
              page={page}
              pageSize={pageSize}
              sorting={sorting}
              onPage={setPage}
              onPageSize={setPageSize}
              onSorting={setSorting}
              empty={
                <ListPageStates
                  icon={<PuzzleIcon />}
                  error={error}
                  loading={skills === null}
                  emptyTitle={t("skills.empty")}
                  emptyHint={t("skills.emptyHint")}
                  emptyAction={
                    <Button size="sm" render={<Link to="/skills/new" />}>
                      <PlusIcon data-icon="inline-start" />
                      {t("skills.add")}
                    </Button>
                  }
                />
              }
            />
            {/* 只在有技能时说：空列表下方挂一句「改动即时生效」是废话。 */}
            {skills && skills.length > 0 ? (
              <p className="mt-4 flex items-center gap-1.5 text-xs text-muted-foreground">
                <InfoIcon className="size-3.5" />
                {t("skills.effectNote")}
              </p>
            ) : null}
          </CardContent>
        </Card>
      </div>

      <AlertDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("skills.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("skills.deleteBody", { name: deleting?.name ?? "" })}
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
