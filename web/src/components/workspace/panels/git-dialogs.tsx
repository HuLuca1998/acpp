import { useState } from "react"
import { useTranslation } from "react-i18next"

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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Textarea } from "@/components/ui/textarea"
import { Input } from "@/components/ui/input"

/** git 面板群共用的「输入一行/一段文字再执行」对话框（新建分支、提交）。 */
export interface GitPrompt {
  title: string
  description?: string
  placeholder?: string
  confirmLabel: string
  /** 多行输入（提交信息用），默认单行。 */
  multiline?: boolean
  defaultValue?: string
  onConfirm: (value: string) => void
}

export function GitPromptDialog({
  prompt,
  onClose,
}: {
  prompt: GitPrompt | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  // 初值只在挂载时取一次：调用方用 key 把不同操作区分开，换操作即重挂，
  // 天然不会带上一次的残留（比在 effect 里同步 setState 干净）。
  const [value, setValue] = useState(prompt?.defaultValue ?? "")

  const submit = () => {
    if (!value.trim()) return
    prompt?.onConfirm(value.trim())
    onClose()
  }

  return (
    <Dialog open={prompt !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{prompt?.title}</DialogTitle>
          {prompt?.description ? (
            <DialogDescription>{prompt.description}</DialogDescription>
          ) : null}
        </DialogHeader>
        {prompt?.multiline ? (
          <Textarea
            value={value}
            autoFocus
            rows={4}
            placeholder={prompt?.placeholder}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => {
              // 多行输入里 Enter 是换行，⌘/Ctrl+Enter 才是提交。
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) submit()
            }}
          />
        ) : (
          <Input
            value={value}
            autoFocus
            className="font-mono text-sm"
            placeholder={prompt?.placeholder}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") submit()
            }}
          />
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button onClick={submit} disabled={!value.trim()}>
            {prompt?.confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** 危险 git 操作的确认（丢弃改动、删分支）：文案讲清后果，不是「确定吗」。 */
export interface GitConfirm {
  title: string
  description: string
  confirmLabel: string
  onConfirm: () => void
}

export function GitConfirmDialog({
  confirm,
  onClose,
}: {
  confirm: GitConfirm | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  return (
    <AlertDialog
      open={confirm !== null}
      onOpenChange={(open) => !open && onClose()}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{confirm?.title}</AlertDialogTitle>
          <AlertDialogDescription>
            {confirm?.description}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            onClick={() => {
              confirm?.onConfirm()
              onClose()
            }}
          >
            {confirm?.confirmLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
