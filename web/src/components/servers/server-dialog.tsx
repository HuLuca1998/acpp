import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { FolderOpenIcon } from "lucide-react"

import { api } from "@/lib/api"
import type { Server, ServerInput, SSHAuth } from "@/types/acp"
import { Hint } from "@/components/hint"
import { DirPicker } from "@/components/dir-picker/dir-picker"
import { TestResult, type TestState } from "@/components/connection-test"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { authLabelKey } from "@/components/servers/auth-label"

/**
 * 服务器的新建/编辑对话框（adr-019）。
 *
 * 表单只有一屏，不分页签——服务器的配置本来就只有「连得上」这一件事，
 * 数据源那边分三页是因为它还要管库与读写。
 */
export function ServerDialog({
  open,
  onOpenChange,
  server,
  prefill,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** null 表示新建。 */
  server: Server | null
  /**
   * 新建时的预填值（从数据源 URI 里解析出的跳板机信息）。只在 server 为
   * null 时有意义——编辑已有记录时它自己的字段就是事实。
   */
  prefill?: ServerInput | null
  onSaved: (saved: Server) => void
}) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {server ? t("server.edit") : t("server.add")}
          </DialogTitle>
          <DialogDescription>{t("server.scopeWarning")}</DialogDescription>
        </DialogHeader>
        {/* key 让对话框每次打开都拿到全新的表单状态：留着上一条的输入
            比空表单更容易误存。 */}
        <ServerForm
          key={server?.id ?? "new"}
          server={server}
          prefill={prefill ?? null}
          onClose={() => onOpenChange(false)}
          onSaved={onSaved}
        />
      </DialogContent>
    </Dialog>
  )
}

function ServerForm({
  server,
  prefill,
  onClose,
  onSaved,
}: {
  server: Server | null
  prefill: ServerInput | null
  onClose: () => void
  onSaved: (saved: Server) => void
}) {
  const { t } = useTranslation()
  const [form, setForm] = useState<ServerInput>({
    name: server?.name ?? prefill?.name ?? "",
    host: server?.host ?? prefill?.host ?? "",
    port: server?.port ?? prefill?.port ?? 22,
    user: server?.user ?? prefill?.user ?? "root",
    auth: server?.auth ?? prefill?.auth ?? "key",
    // 凭证从不下发，编辑时永远从空开始——留空即「不修改」。预填是例外：
    // 那是用户刚粘进来的 URI 自带的，本来就该原样进表单。
    password: prefill?.password ?? "",
    keyPath: server?.keyPath ?? prefill?.keyPath ?? "",
    passphrase: prefill?.passphrase ?? "",
    note: server?.note ?? prefill?.note ?? "",
    disabled: server?.disabled ?? false,
  })
  const [saving, setSaving] = useState(false)
  const [test, setTest] = useState<TestState>({ status: "idle" })
  const [keyPickerOpen, setKeyPickerOpen] = useState(false)

  const set = <K extends keyof ServerInput>(key: K, value: ServerInput[K]) => {
    setForm((prev) => ({ ...prev, [key]: value }))
    // 改了任何字段，上一次的测试结论就作废了。
    setTest({ status: "idle" })
  }

  async function handleTest() {
    setTest({ status: "running" })
    try {
      const res = server
        ? await api.servers.test(server.id, form)
        : await api.servers.probe(form)
      setTest({ status: "ok", version: res.version })
    } catch (err) {
      setTest({ status: "failed", error: (err as Error).message })
    }
  }

  async function handleSave() {
    setSaving(true)
    try {
      const saved = server
        ? await api.servers.update(server.id, form)
        : await api.servers.create(form)
      onSaved(saved)
      toast.success(t("server.saved"))
      onClose()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor="srv-name">{t("server.name")}</FieldLabel>
          <Input
            id="srv-name"
            className="font-mono"
            value={form.name}
            placeholder={t("server.namePlaceholder")}
            onChange={(e) => set("name", e.target.value)}
          />
          <p className="text-xs text-muted-foreground">{t("server.nameHint")}</p>
        </Field>

        <div className="grid grid-cols-[1fr_7rem] gap-4">
          <Field>
            <FieldLabel htmlFor="srv-host">{t("server.host")}</FieldLabel>
            <Input
              id="srv-host"
              className="font-mono"
              value={form.host}
              onChange={(e) => set("host", e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="srv-port">{t("server.port")}</FieldLabel>
            <Input
              id="srv-port"
              type="number"
              value={form.port}
              onChange={(e) => set("port", Number(e.target.value))}
            />
          </Field>
        </div>

        <div className="grid grid-cols-2 gap-4">
          <Field>
            <FieldLabel htmlFor="srv-user">{t("server.user")}</FieldLabel>
            <Input
              id="srv-user"
              className="font-mono"
              value={form.user}
              onChange={(e) => set("user", e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="srv-auth">{t("server.auth")}</FieldLabel>
            <Select
              value={form.auth}
              onValueChange={(v) => set("auth", v as SSHAuth)}
            >
              <SelectTrigger id="srv-auth">
                {/* Base UI 的 Value 默认显示原始 value（会是 "key" 这种），
                    要显示人看的名字得给它一个格式化函数。 */}
                <SelectValue>{(v) => t(authLabelKey(String(v)))}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="password">
                  {t("server.authPassword")}
                </SelectItem>
                <SelectItem value="key">{t("server.authKey")}</SelectItem>
                <SelectItem value="both">{t("server.authBoth")}</SelectItem>
              </SelectContent>
            </Select>
          </Field>
        </div>

        {form.auth !== "key" ? (
          <Field>
            <FieldLabel htmlFor="srv-password">
              {t("server.password")}
            </FieldLabel>
            <Input
              id="srv-password"
              type="password"
              autoComplete="off"
              value={form.password}
              placeholder={server?.hasPassword ? t("server.passwordKeep") : ""}
              onChange={(e) => set("password", e.target.value)}
            />
          </Field>
        ) : null}

        {form.auth !== "password" ? (
          <>
            <Field>
              <FieldLabel htmlFor="srv-key">{t("server.keyPath")}</FieldLabel>
              <div className="flex items-center gap-2">
                <Input
                  id="srv-key"
                  className="flex-1 font-mono"
                  value={form.keyPath}
                  placeholder={t("server.keyPathPlaceholder")}
                  onChange={(e) => set("keyPath", e.target.value)}
                />
                <Hint label={t("server.keyBrowse")} align="end">
                  <Button
                    type="button"
                    variant="outline"
                    size="icon"
                    aria-label={t("server.keyBrowse")}
                    onClick={() => setKeyPickerOpen(true)}
                  >
                    <FolderOpenIcon />
                  </Button>
                </Hint>
              </div>
            </Field>
            <Field>
              <FieldLabel htmlFor="srv-passphrase">
                {t("server.passphrase")}
              </FieldLabel>
              <Input
                id="srv-passphrase"
                type="password"
                autoComplete="off"
                value={form.passphrase}
                placeholder={
                  server?.hasPassphrase ? t("server.passwordKeep") : ""
                }
                onChange={(e) => set("passphrase", e.target.value)}
              />
            </Field>
          </>
        ) : null}

        <Field>
          <FieldLabel htmlFor="srv-note">{t("server.note")}</FieldLabel>
          <Input
            id="srv-note"
            value={form.note}
            placeholder={t("server.notePlaceholder")}
            onChange={(e) => set("note", e.target.value)}
          />
          <p className="text-xs text-muted-foreground">{t("server.noteHint")}</p>
        </Field>

        <div className="flex items-center gap-2">
          <Switch
            id="srv-disabled"
            checked={form.disabled}
            onCheckedChange={(v) => set("disabled", v)}
          />
          <FieldLabel htmlFor="srv-disabled">{t("server.disabled")}</FieldLabel>
        </div>

        <p className="text-xs text-muted-foreground">
          {t("server.knownHosts")}
        </p>
      </FieldGroup>

      <TestResult state={test} variant="ssh" />

      <DialogFooter className="gap-2 sm:justify-between">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={test.status === "running"}
          onClick={handleTest}
        >
          {test.status === "running" ? <Spinner /> : null}
          {test.status === "running" ? t("db.testing") : t("server.test")}
        </Button>
        <div className="flex gap-2">
          <Button variant="outline" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button disabled={saving} onClick={handleSave}>
            {saving ? <Spinner /> : null}
            {t("common.save")}
          </Button>
        </div>
      </DialogFooter>

      <DirPicker
        open={keyPickerOpen}
        onOpenChange={setKeyPickerOpen}
        mode="file"
        initialPath={
          (form.keyPath ?? "").includes("/")
            ? (form.keyPath ?? "").slice(0, (form.keyPath ?? "").lastIndexOf("/")) ||
              "~/.ssh"
            : "~/.ssh"
        }
        onSelect={(path) => {
          set("keyPath", path)
          setKeyPickerOpen(false)
        }}
      />
    </>
  )
}
