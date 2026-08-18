import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { api } from "@/lib/api"
import { formatBytes, formatDateTime, formatRelativeTime } from "@/lib/format"
import type { SkillFile } from "@/types/acp"
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
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"
import { FileTextIcon, PlusIcon, Trash2Icon } from "lucide-react"

/** 附属文件管理：文本文件可就地编辑，二进制只列出。改动后回调 onChanged. */
export function SkillFiles({
  skillName,
  onChanged,
}: {
  skillName: string
  onChanged: () => void
}) {
  const { t, i18n } = useTranslation()
  const [files, setFiles] = useState<SkillFile[] | null>(null)
  const [editing, setEditing] = useState<{
    path: string
    isNew: boolean
  } | null>(null)
  const [deleting, setDeleting] = useState<string | null>(null)

  const reload = useCallback(() => {
    api.skills
      .files(skillName)
      .then((res) => setFiles(res.items))
      .catch((err: Error) => toast.error(err.message))
  }, [skillName])

  useEffect(reload, [reload])

  function changed() {
    reload()
    onChanged()
  }

  async function remove() {
    if (!deleting) return
    try {
      await api.skills.removeFile(skillName, deleting)
      changed()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setDeleting(null)
    }
  }

  if (files === null) {
    return <Skeleton className="h-16 w-full" />
  }

  return (
    <div className="flex flex-col gap-2">
      {files.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {t("skills.detail.noFiles")}
        </p>
      ) : (
        <ul className="flex flex-col divide-y">
          {files.map((file) => (
            <li key={file.path} className="group flex items-center gap-3 py-2">
              <FileTextIcon className="size-4 text-muted-foreground" />
              {file.binary ? (
                <span className="font-mono text-xs">{file.path}</span>
              ) : (
                <button
                  type="button"
                  className="font-mono text-xs hover:underline"
                  onClick={() => setEditing({ path: file.path, isNew: false })}
                >
                  {file.path}
                </button>
              )}
              {file.binary && (
                <Badge variant="secondary">{t("skills.detail.binary")}</Badge>
              )}
              <span className="ml-auto text-xs text-muted-foreground tabular-nums">
                {formatBytes(file.size)}
              </span>
              <span
                className="text-xs text-muted-foreground tabular-nums"
                title={formatDateTime(file.updatedAt, i18n.language)}
              >
                {formatRelativeTime(file.updatedAt, i18n.language)}
              </span>
              <Button
                size="icon-sm"
                variant="ghost"
                className="text-muted-foreground opacity-0 group-hover:opacity-100 hover:text-destructive focus-visible:opacity-100"
                aria-label={t("common.delete")}
                onClick={() => setDeleting(file.path)}
              >
                <Trash2Icon />
              </Button>
            </li>
          ))}
        </ul>
      )}
      <div>
        <Button
          size="sm"
          variant="outline"
          onClick={() => setEditing({ path: "", isNew: true })}
        >
          <PlusIcon data-icon="inline-start" />
          {t("skills.detail.newFile")}
        </Button>
      </div>

      {editing && (
        <FileEditorDialog
          skillName={skillName}
          path={editing.path}
          isNew={editing.isNew}
          onClose={(saved) => {
            setEditing(null)
            if (saved) changed()
          }}
        />
      )}

      <AlertDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("skills.detail.fileDeleteTitle")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t("skills.detail.fileDeleteBody", { path: deleting ?? "" })}
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

/** 文本文件编辑器：新建时路径可填，编辑时只读展示。 */
function FileEditorDialog({
  skillName,
  path,
  isNew,
  onClose,
}: {
  skillName: string
  path: string
  isNew: boolean
  onClose: (saved: boolean) => void
}) {
  const { t } = useTranslation()
  const [filePath, setFilePath] = useState(path)
  const [content, setContent] = useState<string | null>(isNew ? "" : null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (isNew) return
    api.skills
      .file(skillName, path)
      .then((res) => setContent(res.content))
      .catch((err: Error) => {
        toast.error(err.message)
        onClose(false)
      })
    // onClose 是父组件的临时闭包，不进依赖。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [skillName, path, isNew])

  async function save() {
    setSaving(true)
    try {
      await api.skills.putFile(skillName, filePath.trim(), content ?? "")
      onClose(true)
    } catch (err) {
      toast.error((err as Error).message)
      setSaving(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose(false)}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="font-mono text-sm">
            {isNew ? t("skills.detail.newFile") : filePath}
          </DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          {isNew && (
            <Input
              value={filePath}
              onChange={(e) => setFilePath(e.target.value)}
              placeholder={t("skills.detail.filePathPlaceholder")}
              aria-label={t("skills.detail.filePath")}
              className="font-mono"
              autoFocus
            />
          )}
          {content === null ? (
            <Skeleton className="h-64 w-full" />
          ) : (
            <Textarea
              value={content}
              onChange={(e) => setContent(e.target.value)}
              className="min-h-64 font-mono text-xs leading-relaxed"
            />
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onClose(false)}>
            {t("common.cancel")}
          </Button>
          <Button
            disabled={saving || content === null || filePath.trim() === ""}
            onClick={save}
          >
            {t("common.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
