import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { SSHKey, SSHKeyInput } from "@/types/acp"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { PasswordInput } from "@/components/password-input"
import { Textarea } from "@/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"

/** 私钥的三种来源。生成在前：多数时候人要的就是一把新的。 */
type Source = "generate" | "paste" | "file"

/**
 * 新建 / 编辑一把私钥。
 *
 * 编辑时私钥内容留空表示不动原来的——与密码字段同一套约定（凭证永不下发，
 * 表单里那一格本来就是空的，按字面处理会把钥匙清掉）。
 */
export function SSHKeyDialog({
  sshKey,
  onClose,
  onSaved,
}: {
  sshKey: SSHKey | null
  onClose: () => void
  onSaved: (saved: SSHKey) => void
}) {
  const { t } = useTranslation()
  const editing = sshKey !== null
  const [source, setSource] = useState<Source>(editing ? "paste" : "generate")
  const [form, setForm] = useState({
    name: sshKey?.name ?? "",
    note: sshKey?.note ?? "",
    privateKey: "",
    keyPath: "",
    passphrase: "",
  })
  const [saving, setSaving] = useState(false)

  const set = (key: keyof typeof form, value: string) =>
    setForm((prev) => ({ ...prev, [key]: value }))

  async function save() {
    setSaving(true)
    try {
      const input: SSHKeyInput = {
        name: form.name.trim(),
        note: form.note.trim(),
        passphrase: form.passphrase,
      }
      if (source === "generate" && !editing) input.generate = true
      if (source === "paste") input.privateKey = form.privateKey.trim()
      if (source === "file") input.keyPath = form.keyPath.trim()
      const saved = editing
        ? await api.sshKeys.update(sshKey.id, input)
        : await api.sshKeys.create(input)
      toast.success(t(editing ? "sshKeys.saved" : "sshKeys.created"))
      onSaved(saved)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  // 编辑时不填任何私钥材料也能保存（只改名字/备注）。
  const materialReady =
    editing ||
    (source === "generate" && true) ||
    (source === "paste" && form.privateKey.trim() !== "") ||
    (source === "file" && form.keyPath.trim() !== "")

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {t(editing ? "sshKeys.editTitle" : "sshKeys.addTitle")}
          </DialogTitle>
          <DialogDescription>{t("sshKeys.dialogHint")}</DialogDescription>
        </DialogHeader>

        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="key-name">{t("sshKeys.name")}</FieldLabel>
            <Input
              id="key-name"
              value={form.name}
              onChange={(e) => set("name", e.target.value)}
              placeholder={t("sshKeys.namePlaceholder")}
              className="font-mono"
              autoFocus
            />
            <FieldDescription>{t("sshKeys.nameHint")}</FieldDescription>
          </Field>

          {!editing && (
            <Field>
              <FieldLabel>{t("sshKeys.source")}</FieldLabel>
              <ToggleGroup
                value={[source]}
                onValueChange={(v) => v[0] && setSource(v[0] as Source)}
                className="w-fit"
              >
                <ToggleGroupItem value="generate">
                  {t("sshKeys.sourceGenerate")}
                </ToggleGroupItem>
                <ToggleGroupItem value="paste">
                  {t("sshKeys.sourcePaste")}
                </ToggleGroupItem>
                <ToggleGroupItem value="file">
                  {t("sshKeys.sourceFile")}
                </ToggleGroupItem>
              </ToggleGroup>
              <FieldDescription>
                {t(`sshKeys.sourceHint.${source}`)}
              </FieldDescription>
            </Field>
          )}

          {source === "paste" && (
            <Field>
              <FieldLabel htmlFor="key-pem">
                {t("sshKeys.privateKey")}
              </FieldLabel>
              <Textarea
                id="key-pem"
                value={form.privateKey}
                onChange={(e) => set("privateKey", e.target.value)}
                placeholder={
                  editing
                    ? t("sshKeys.privateKeyKeep")
                    : "-----BEGIN OPENSSH PRIVATE KEY-----"
                }
                className="min-h-32 font-mono text-xs"
              />
            </Field>
          )}

          {source === "file" && (
            <Field>
              <FieldLabel htmlFor="key-path">{t("sshKeys.keyPath")}</FieldLabel>
              <Input
                id="key-path"
                value={form.keyPath}
                onChange={(e) => set("keyPath", e.target.value)}
                placeholder="~/.ssh/id_ed25519"
                className="font-mono"
              />
              <FieldDescription>{t("sshKeys.keyPathHint")}</FieldDescription>
            </Field>
          )}

          <Field>
            <FieldLabel htmlFor="key-phrase">
              {t("sshKeys.passphrase")}
            </FieldLabel>
            <PasswordInput
              id="key-phrase"
              value={form.passphrase}
              placeholder={
                sshKey?.hasPassphrase ? t("sshKeys.passphraseKeep") : ""
              }
              onChange={(v) => set("passphrase", v)}
              fetchStored={
                sshKey?.hasPassphrase
                  ? async () => (await api.sshKeys.secret(sshKey.id)).passphrase
                  : undefined
              }
            />
            <FieldDescription>{t("sshKeys.passphraseHint")}</FieldDescription>
          </Field>

          <Field>
            <FieldLabel htmlFor="key-note">{t("sshKeys.note")}</FieldLabel>
            <Input
              id="key-note"
              value={form.note}
              onChange={(e) => set("note", e.target.value)}
              placeholder={t("sshKeys.notePlaceholder")}
            />
          </Field>
        </FieldGroup>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button
            disabled={saving || form.name.trim() === "" || !materialReady}
            onClick={save}
          >
            {t("common.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
