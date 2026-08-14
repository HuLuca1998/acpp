import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"
import { toast } from "sonner"

import { ListPageStates } from "@/components/list-page-states"
import { useAsyncData } from "@/hooks/use-async-data"

import { api } from "@/lib/api"
import { formatDateTime, formatRelativeTime } from "@/lib/format"
import type { Skill } from "@/types/acp"
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { InfoIcon, PlusIcon, PuzzleIcon, Trash2Icon } from "lucide-react"

export function Skills() {
  const { t, i18n } = useTranslation()
  const {
    data: skills,
    error,
    setData: setSkills,
  } = useAsyncData(() => api.skills.list().then((res) => res.items), [])
  const [deleting, setDeleting] = useState<Skill | null>(null)

  async function toggle(skill: Skill, enabled: boolean) {
    // 乐观更新：符号链接切换基本不会失败，失败时回滚并提示。
    setSkills(
      (prev) =>
        prev?.map((s) => (s.name === skill.name ? { ...s, enabled } : s)) ??
        null
    )
    try {
      await api.skills.update(skill.name, { enabled })
      toast.success(
        t(enabled ? "skills.enabledToast" : "skills.disabledToast", {
          name: skill.name,
        }),
        { description: t("skills.effectNote") }
      )
    } catch (err) {
      setSkills(
        (prev) =>
          prev?.map((s) =>
            s.name === skill.name ? { ...s, enabled: !enabled } : s
          ) ?? null
      )
      toast.error((err as Error).message)
    }
  }

  async function remove() {
    if (!deleting) return
    try {
      await api.skills.remove(deleting.name)
      setSkills((prev) => prev?.filter((s) => s.name !== deleting.name) ?? null)
      toast.success(t("skills.deleted"))
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
            {error || skills === null || skills.length === 0 ? (
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
            ) : (
              <>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t("skills.name")}</TableHead>
                      <TableHead>{t("skills.colDescription")}</TableHead>
                      <TableHead className="text-right">
                        {t("skills.usage")}
                      </TableHead>
                      <TableHead className="text-right">
                        {t("skills.updated")}
                      </TableHead>
                      <TableHead className="w-24 text-right">
                        {t("skills.enabled")}
                      </TableHead>
                      <TableHead className="w-10" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {skills.map((skill) => (
                      <TableRow key={skill.name} className="group relative">
                        <TableCell className="font-mono text-xs font-medium">
                          {/* 拉伸链接铺满整行；开关与删除在链接之上单独可点。 */}
                          <Link
                            to={`/skills/${skill.name}`}
                            className="after:absolute after:inset-0"
                          >
                            {skill.name}
                          </Link>
                        </TableCell>
                        <TableCell className="max-w-96 truncate text-muted-foreground">
                          {skill.description || t("common.none")}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {skill.usageCount > 0 ? (
                            skill.usageCount.toLocaleString()
                          ) : (
                            <span className="text-muted-foreground">
                              {t("common.none")}
                            </span>
                          )}
                        </TableCell>
                        <TableCell
                          className="text-right text-muted-foreground tabular-nums"
                          title={formatDateTime(skill.updatedAt, i18n.language)}
                        >
                          {formatRelativeTime(skill.updatedAt, i18n.language)}
                        </TableCell>
                        <TableCell className="text-right">
                          <Switch
                            className="relative"
                            checked={skill.enabled}
                            onCheckedChange={(on) => toggle(skill, on)}
                            aria-label={t("skills.enabled")}
                          />
                        </TableCell>
                        <TableCell className="text-right">
                          <Button
                            size="icon-sm"
                            variant="ghost"
                            className="relative text-muted-foreground opacity-0 group-hover:opacity-100 hover:text-destructive focus-visible:opacity-100"
                            aria-label={t("common.delete")}
                            onClick={() => setDeleting(skill)}
                          >
                            <Trash2Icon />
                          </Button>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
                <p className="mt-4 flex items-center gap-1.5 text-xs text-muted-foreground">
                  <InfoIcon className="size-3.5" />
                  {t("skills.effectNote")}
                </p>
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
