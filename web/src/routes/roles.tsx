import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { ListPageStates } from "@/components/list-page-states"
import { AgentIcon } from "@/components/agent-icon"
import { RoleDialog } from "@/components/roles/role-dialog"
import { useAsyncData } from "@/hooks/use-async-data"
import { api } from "@/lib/api"
import type { Role } from "@/types/acp"
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { DramaIcon, PlusIcon, Trash2Icon } from "lucide-react"

/**
 * 角色页（adr-006）：编排里可雇佣的子代理定义。列表 + Dialog 编辑，
 * 角色是轻量配置对象，不配详情路由。
 */
export function Roles() {
  const { t } = useTranslation()
  const {
    data: roles,
    error,
    setData: setRoles,
  } = useAsyncData(() => api.roles.list(), [])
  const { data: agents } = useAsyncData(
    () => api.agents.list().then((res) => res.items),
    []
  )
  const [editing, setEditing] = useState<Role | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleting, setDeleting] = useState<Role | null>(null)

  function agentName(id: number) {
    return agents?.find((a) => a.id === id)?.name ?? `#${id}`
  }

  function agentFlavor(id: number) {
    return agents?.find((a) => a.id === id)?.flavor
  }

  function openEdit(role: Role | null) {
    setEditing(role)
    setDialogOpen(true)
  }

  function onSaved(saved: Role) {
    setRoles((prev) => {
      if (!prev) return [saved]
      const index = prev.findIndex((r) => r.id === saved.id)
      if (index < 0) return [...prev, saved]
      const next = [...prev]
      next[index] = saved
      return next
    })
  }

  async function remove() {
    if (!deleting) return
    try {
      await api.roles.remove(deleting.id)
      setRoles((prev) => prev?.filter((r) => r.id !== deleting.id) ?? null)
      toast.success(t("roles.deleted"))
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
            <CardTitle>{t("roles.title")}</CardTitle>
            <CardDescription>{t("roles.description")}</CardDescription>
            <CardAction>
              <Button size="sm" onClick={() => openEdit(null)}>
                <PlusIcon data-icon="inline-start" />
                {t("roles.add")}
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            {error || roles === null || roles.length === 0 ? (
              <ListPageStates
                icon={<DramaIcon />}
                error={error}
                loading={roles === null}
                emptyTitle={t("roles.empty")}
                emptyHint={t("roles.emptyHint")}
                emptyAction={
                  <Button size="sm" onClick={() => openEdit(null)}>
                    <PlusIcon data-icon="inline-start" />
                    {t("roles.add")}
                  </Button>
                }
              />
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("roles.name")}</TableHead>
                    <TableHead>{t("roles.colDescription")}</TableHead>
                    <TableHead>{t("roles.agent")}</TableHead>
                    <TableHead>{t("roles.model")}</TableHead>
                    <TableHead>{t("roles.level")}</TableHead>
                    <TableHead className="w-10" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {roles.map((role) => (
                    <TableRow key={role.id} className="group relative">
                      <TableCell className="font-medium">
                        {/* 整行可点开编辑（拉伸按钮模式），删除在其上单独可点。 */}
                        <button
                          type="button"
                          className="after:absolute after:inset-0"
                          onClick={() => openEdit(role)}
                        >
                          {role.name}
                        </button>
                        {role.builtin ? (
                          <Badge variant="secondary" className="ml-2">
                            {t("roles.builtin")}
                          </Badge>
                        ) : null}
                      </TableCell>
                      <TableCell className="max-w-96 truncate text-muted-foreground">
                        {role.description || t("common.none")}
                      </TableCell>
                      <TableCell>
                        <span className="inline-flex items-center gap-1.5">
                          <AgentIcon
                            flavor={agentFlavor(role.agentId)}
                            className="size-4"
                          />
                          {agentName(role.agentId)}
                        </span>
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {role.model || t("roles.default")}
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {role.level
                          ? t(`chat.settings.level.${role.level}` as never, {
                              defaultValue: role.level,
                            })
                          : t("roles.default")}
                      </TableCell>
                      <TableCell className="text-right">
                        <Button
                          size="icon-sm"
                          variant="ghost"
                          className="relative text-muted-foreground opacity-0 group-hover:opacity-100 hover:text-destructive focus-visible:opacity-100"
                          aria-label={t("common.delete")}
                          onClick={() => setDeleting(role)}
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

      <RoleDialog
        open={dialogOpen}
        role={editing}
        agents={agents ?? []}
        onClose={() => setDialogOpen(false)}
        onSaved={onSaved}
      />

      <AlertDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("roles.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("roles.deleteBody", { name: deleting?.name ?? "" })}
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
