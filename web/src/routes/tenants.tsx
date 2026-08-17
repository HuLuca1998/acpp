import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { ListPageStates } from "@/components/list-page-states"
import { StatusDot } from "@/components/status-dot"
import { useAsyncData } from "@/hooks/use-async-data"
import { api } from "@/lib/api"
import { formatRelativeTime } from "@/lib/format"
import type { Tenant } from "@/types/acp"
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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  CopyIcon,
  MoreHorizontalIcon,
  PlusIcon,
  RefreshCwIcon,
  Trash2Icon,
  UsersIcon,
} from "lucide-react"

/**
 * 连接页 = 多租户管理（adr-007）。owner 在这里建人、发分享链接、随时
 * 关停。租户凭链接换到的 cookie 长期有效，收回访问靠停用而不是等过期。
 */
export function Tenants() {
  const { t, i18n } = useTranslation()
  const {
    data: tenants,
    error,
    setData: setTenants,
  } = useAsyncData(() => api.tenants.list().then((res) => res.items), [])

  const [creating, setCreating] = useState(false)
  const [newName, setNewName] = useState("")
  const [createError, setCreateError] = useState<string | null>(null)
  const [deleting, setDeleting] = useState<Tenant | null>(null)
  const [rotating, setRotating] = useState<Tenant | null>(null)

  function upsert(saved: Tenant) {
    setTenants((prev) => {
      if (!prev) return [saved]
      const index = prev.findIndex((item) => item.id === saved.id)
      if (index < 0) return [...prev, saved]
      const next = [...prev]
      next[index] = saved
      return next
    })
  }

  async function copyInvite(tenant: Tenant) {
    try {
      await navigator.clipboard.writeText(tenant.inviteUrl)
      toast.success(t("tenants.linkCopied"))
    } catch {
      // 剪贴板在非安全上下文里不可用（局域网 http 访问就是这种情况），
      // 退而把链接摆出来让人手动复制。
      toast.info(tenant.inviteUrl)
    }
  }

  async function create() {
    const name = newName.trim()
    if (!name) return
    try {
      const created = await api.tenants.create(name)
      upsert(created)
      setCreating(false)
      setNewName("")
      setCreateError(null)
      await copyInvite(created)
    } catch (err) {
      setCreateError((err as Error).message)
    }
  }

  async function toggle(tenant: Tenant) {
    try {
      const updated = await api.tenants.update(tenant.id, {
        disabled: !tenant.disabled,
      })
      // update 返回的是裸租户（没有 inviteUrl），保留原链接不覆盖。
      upsert({ ...tenant, ...updated, inviteUrl: tenant.inviteUrl })
      toast.success(
        tenant.disabled ? t("tenants.enabled") : t("tenants.disabled")
      )
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  async function rotate() {
    if (!rotating) return
    try {
      const updated = await api.tenants.rotate(rotating.id)
      upsert(updated)
      await copyInvite(updated)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setRotating(null)
    }
  }

  async function remove() {
    if (!deleting) return
    try {
      await api.tenants.remove(deleting.id)
      setTenants((prev) => prev?.filter((item) => item.id !== deleting.id) ?? null)
      toast.success(t("tenants.removed"))
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
            <CardTitle>{t("tenants.title")}</CardTitle>
            <CardDescription>{t("tenants.description")}</CardDescription>
            <CardAction>
              <Button size="sm" onClick={() => setCreating(true)}>
                <PlusIcon data-icon="inline-start" />
                {t("tenants.add")}
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            {error || tenants === null || tenants.length === 0 ? (
              <ListPageStates
                icon={<UsersIcon />}
                error={error}
                loading={tenants === null}
                emptyTitle={t("tenants.empty")}
                emptyHint={t("tenants.emptyHint")}
                emptyAction={
                  <Button size="sm" onClick={() => setCreating(true)}>
                    <PlusIcon data-icon="inline-start" />
                    {t("tenants.add")}
                  </Button>
                }
              />
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("tenants.name")}</TableHead>
                    <TableHead>{t("tenants.root")}</TableHead>
                    <TableHead className="text-right">
                      {t("tenants.sessions")}
                    </TableHead>
                    <TableHead>{t("tenants.lastSeen")}</TableHead>
                    <TableHead className="w-10" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {tenants.map((tenant) => (
                    <TableRow key={tenant.id}>
                      <TableCell className="font-medium">
                        <span className="inline-flex items-center gap-2">
                          <StatusDot
                            tone={tenant.disabled ? "muted" : "success"}
                          />
                          {tenant.name}
                        </span>
                      </TableCell>
                      <TableCell className="max-w-80 truncate font-mono text-xs text-muted-foreground">
                        {tenant.root}
                      </TableCell>
                      <TableCell className="text-right tabular-nums">
                        {tenant.sessionCount}
                      </TableCell>
                      <TableCell className="text-muted-foreground tabular-nums">
                        {tenant.lastSeenAt
                          ? formatRelativeTime(tenant.lastSeenAt, i18n.language)
                          : t("tenants.never")}
                      </TableCell>
                      <TableCell className="text-right">
                        <DropdownMenu>
                          <DropdownMenuTrigger
                            render={
                              <Button
                                size="icon-sm"
                                variant="ghost"
                                className="text-muted-foreground"
                                aria-label={t("tenants.actions")}
                              />
                            }
                          >
                            <MoreHorizontalIcon />
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end" className="w-52">
                            <DropdownMenuItem
                              onClick={() => void copyInvite(tenant)}
                            >
                              <CopyIcon />
                              <span>{t("tenants.copyLink")}</span>
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => setRotating(tenant)}>
                              <RefreshCwIcon />
                              <span>{t("tenants.rotate")}</span>
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem onClick={() => void toggle(tenant)}>
                              <StatusDot
                                tone={tenant.disabled ? "success" : "muted"}
                              />
                              <span>
                                {tenant.disabled
                                  ? t("tenants.enable")
                                  : t("tenants.disable")}
                              </span>
                            </DropdownMenuItem>
                            <DropdownMenuItem
                              variant="destructive"
                              onClick={() => setDeleting(tenant)}
                            >
                              <Trash2Icon />
                              <span>{t("common.delete")}</span>
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      </div>

      <Dialog open={creating} onOpenChange={setCreating}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("tenants.add")}</DialogTitle>
            <DialogDescription>{t("tenants.addDesc")}</DialogDescription>
          </DialogHeader>
          <Field>
            <FieldLabel htmlFor="tenant-name">{t("tenants.name")}</FieldLabel>
            <Input
              id="tenant-name"
              value={newName}
              autoFocus
              placeholder={t("tenants.namePlaceholder")}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") void create()
              }}
            />
            <FieldDescription>
              {createError ?? t("tenants.nameHint")}
            </FieldDescription>
          </Field>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreating(false)}>
              {t("common.cancel")}
            </Button>
            <Button onClick={() => void create()} disabled={!newName.trim()}>
              {t("tenants.addAndCopy")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={rotating !== null}
        onOpenChange={(open) => !open && setRotating(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("tenants.rotate")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("tenants.rotateConfirm", { name: rotating?.name ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction onClick={() => void rotate()}>
              {t("tenants.rotate")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("tenants.removeTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("tenants.removeConfirm", { name: deleting?.name ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => void remove()}
            >
              {t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
