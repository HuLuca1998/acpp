import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { api, ApiError } from "@/lib/api"
import type {
  DataSource,
  DataSourceInput,
  DbDatabase,
  SSHAuth,
} from "@/types/acp"
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
import { SuggestInput } from "@/components/db/suggest-input"
import { UriDialog } from "@/components/db/uri-dialog"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Spinner } from "@/components/ui/spinner"
import { CheckCircle2Icon, XCircleIcon } from "lucide-react"

/**
 * 连接编辑对话框：常规 / SSH / 高级三个页签（形态照 Navicat，做同一件事
 * 的工具长得像，用户不用重新学）。新建与编辑共用，表单状态由 key 驱动
 * 重挂初始化。
 *
 * 密码类字段留空 = 不修改：编辑已有连接时后端不下发密码，界面只知道
 * 「设过」，所以不能把空串当成「清空密码」提交上去。
 */
export function DataSourceDialog({
  open,
  source,
  projects,
  onClose,
  onSaved,
}: {
  open: boolean
  source: DataSource | null
  /** 工作区里已有的项目名，填「项目」时给建议。 */
  projects: string[]
  onClose: () => void
  onSaved: (source: DataSource) => void
}) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>
            {source ? t("db.editTitle") : t("db.addTitle")}
          </DialogTitle>
          <DialogDescription>{t("db.projectHint")}</DialogDescription>
        </DialogHeader>
        {open ? (
          <DataSourceForm
            key={source?.id ?? "new"}
            source={source}
            projects={projects}
            onClose={onClose}
            onSaved={onSaved}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

type TestState =
  | { status: "idle" }
  | { status: "running" }
  | { status: "ok"; version?: string }
  | { status: "failed"; error: string }

function DataSourceForm({
  source,
  projects,
  onClose,
  onSaved,
}: {
  source: DataSource | null
  projects: string[]
  onClose: () => void
  onSaved: (source: DataSource) => void
}) {
  const { t } = useTranslation()
  const [form, setForm] = useState<DataSourceInput>(() => ({
    project: source?.project ?? "",
    env: source?.env ?? "",
    host: source?.host ?? "127.0.0.1",
    port: source?.port ?? 3306,
    user: source?.user ?? "root",
    password: "",
    database: source?.database ?? "",
    params: source?.params ?? "",
    note: source?.note ?? "",
    sshEnabled: source?.sshEnabled ?? false,
    sshHost: source?.sshHost ?? "",
    sshPort: source?.sshPort ?? 22,
    sshUser: source?.sshUser ?? "",
    sshAuth: source?.sshAuth ?? "password",
    sshPassword: "",
    sshKeyPath: source?.sshKeyPath ?? "",
    sshPassphrase: "",
    readOnly: source?.readOnly ?? true,
    disabled: source?.disabled ?? false,
  }))
  const [saving, setSaving] = useState(false)
  const [test, setTest] = useState<TestState>({ status: "idle" })
  const [uriOpen, setUriOpen] = useState(false)

  const set = <K extends keyof DataSourceInput>(
    key: K,
    value: DataSourceInput[K]
  ) => {
    setForm((prev) => ({ ...prev, [key]: value }))
    // 改了任何字段，上一次的测试结论就作废了。
    setTest({ status: "idle" })
  }

  async function save(): Promise<DataSource | null> {
    setSaving(true)
    try {
      const saved = source
        ? await api.datasources.update(source.id, form)
        : await api.datasources.create(form)
      toast.success(source ? t("db.updated") : t("db.created"))
      return saved
    } catch (err) {
      toast.error(
        err instanceof ApiError ? err.message : String((err as Error).message)
      )
      return null
    } finally {
      setSaving(false)
    }
  }

  // 测试连接得先落库：连接要用到密码，而密码只在后端手上（编辑时界面
  // 根本没有它）。先存再测，也免得测通了却忘了保存。
  async function handleTest() {
    setTest({ status: "running" })
    const saved = await save()
    if (!saved) {
      setTest({ status: "idle" })
      return
    }
    onSaved(saved)
    try {
      const result = await api.datasources.test(saved.id)
      setTest(
        result.ok
          ? { status: "ok", version: result.version }
          : { status: "failed", error: result.error ?? "" }
      )
    } catch (err) {
      setTest({ status: "failed", error: (err as Error).message })
    }
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const saved = await save()
    if (saved) {
      onSaved(saved)
      onClose()
    }
  }

  return (
    <form onSubmit={handleSubmit} className="flex flex-col gap-4">
      <Tabs defaultValue="general">
        <TabsList>
          <TabsTrigger value="general">{t("db.tabGeneral")}</TabsTrigger>
          <TabsTrigger value="ssh">{t("db.tabSSH")}</TabsTrigger>
          <TabsTrigger value="advanced">{t("db.tabAdvanced")}</TabsTrigger>
        </TabsList>

        <TabsContent value="general">
          <FieldGroup>
            <div className="grid grid-cols-2 gap-4">
              <Field>
                <FieldLabel htmlFor="ds-project">{t("db.project")}</FieldLabel>
                {/* 建议而非限制：库常常先于代码存在，本机还没 clone 也得能配。 */}
                <SuggestInput
                  id="ds-project"
                  required
                  value={form.project}
                  options={projects}
                  placeholder={t("db.projectPlaceholder")}
                  onChange={(v) => set("project", v)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="ds-env">{t("db.env")}</FieldLabel>
                <Input
                  id="ds-env"
                  required
                  value={form.env}
                  placeholder={t("db.envPlaceholder")}
                  onChange={(e) => set("env", e.target.value)}
                />
              </Field>
            </div>

            <div className="grid grid-cols-[1fr_7rem] gap-4">
              <Field>
                <FieldLabel htmlFor="ds-host">{t("db.host")}</FieldLabel>
                <Input
                  id="ds-host"
                  required
                  value={form.host}
                  onChange={(e) => set("host", e.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="ds-port">{t("db.port")}</FieldLabel>
                <Input
                  id="ds-port"
                  type="number"
                  value={form.port}
                  onChange={(e) => set("port", Number(e.target.value))}
                />
              </Field>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <Field>
                <FieldLabel htmlFor="ds-user">{t("db.user")}</FieldLabel>
                <Input
                  id="ds-user"
                  required
                  value={form.user}
                  onChange={(e) => set("user", e.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="ds-password">
                  {t("db.password")}
                </FieldLabel>
                <Input
                  id="ds-password"
                  type="password"
                  autoComplete="off"
                  value={form.password}
                  placeholder={source?.hasPassword ? t("db.passwordKeep") : ""}
                  onChange={(e) => set("password", e.target.value)}
                />
              </Field>
            </div>

            {/* 一条连接只对应一个库，所以这里是必选而不是可填：
                选定之后所有入口（界面、斜杠命令、AI）都锁死在它上面。 */}
            <DatabasePicker
              sourceId={source?.id ?? 0}
              form={form}
              onPick={(name) => set("database", name)}
            />

          </FieldGroup>
        </TabsContent>

        <TabsContent value="ssh">
          <FieldGroup>
            <div className="flex items-center gap-2">
              <Switch
                id="ds-ssh"
                checked={form.sshEnabled}
                onCheckedChange={(v) => set("sshEnabled", v)}
              />
              <FieldLabel htmlFor="ds-ssh">{t("db.ssh")}</FieldLabel>
            </div>

            {form.sshEnabled ? (
              <>
                <p className="text-xs text-muted-foreground">
                  {t("db.sshHint")}
                </p>
                <div className="grid grid-cols-[1fr_7rem] gap-4">
                  <Field>
                    <FieldLabel htmlFor="ds-ssh-host">
                      {t("db.sshHost")}
                    </FieldLabel>
                    <Input
                      id="ds-ssh-host"
                      value={form.sshHost}
                      onChange={(e) => set("sshHost", e.target.value)}
                    />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="ds-ssh-port">
                      {t("db.sshPort")}
                    </FieldLabel>
                    <Input
                      id="ds-ssh-port"
                      type="number"
                      value={form.sshPort}
                      onChange={(e) => set("sshPort", Number(e.target.value))}
                    />
                  </Field>
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <Field>
                    <FieldLabel htmlFor="ds-ssh-user">
                      {t("db.sshUser")}
                    </FieldLabel>
                    <Input
                      id="ds-ssh-user"
                      value={form.sshUser}
                      onChange={(e) => set("sshUser", e.target.value)}
                    />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="ds-ssh-auth">
                      {t("db.sshAuth")}
                    </FieldLabel>
                    <Select
                      value={form.sshAuth}
                      onValueChange={(v) => set("sshAuth", v as SSHAuth)}
                    >
                      <SelectTrigger id="ds-ssh-auth">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="password">
                          {t("db.sshAuthPassword")}
                        </SelectItem>
                        <SelectItem value="key">
                          {t("db.sshAuthKey")}
                        </SelectItem>
                        <SelectItem value="both">
                          {t("db.sshAuthBoth")}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  </Field>
                </div>

                {form.sshAuth !== "key" ? (
                  <Field>
                    <FieldLabel htmlFor="ds-ssh-password">
                      {t("db.sshPassword")}
                    </FieldLabel>
                    <Input
                      id="ds-ssh-password"
                      type="password"
                      autoComplete="off"
                      value={form.sshPassword}
                      placeholder={
                        source?.hasSSHPassword ? t("db.passwordKeep") : ""
                      }
                      onChange={(e) => set("sshPassword", e.target.value)}
                    />
                  </Field>
                ) : null}

                {form.sshAuth !== "password" ? (
                  <>
                    <Field>
                      <FieldLabel htmlFor="ds-ssh-key">
                        {t("db.sshKeyPath")}
                      </FieldLabel>
                      <Input
                        id="ds-ssh-key"
                        className="font-mono"
                        value={form.sshKeyPath}
                        placeholder={t("db.sshKeyPathPlaceholder")}
                        onChange={(e) => set("sshKeyPath", e.target.value)}
                      />
                    </Field>
                    <Field>
                      <FieldLabel htmlFor="ds-ssh-passphrase">
                        {t("db.sshPassphrase")}
                      </FieldLabel>
                      <Input
                        id="ds-ssh-passphrase"
                        type="password"
                        autoComplete="off"
                        value={form.sshPassphrase}
                        placeholder={
                          source?.hasSSHPassphrase ? t("db.passwordKeep") : ""
                        }
                        onChange={(e) => set("sshPassphrase", e.target.value)}
                      />
                    </Field>
                  </>
                ) : null}

                <p className="text-xs text-muted-foreground">
                  {t("db.sshKnownHosts")}
                </p>
              </>
            ) : null}
          </FieldGroup>
        </TabsContent>

        <TabsContent value="advanced">
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="ds-params">{t("db.params")}</FieldLabel>
              <Input
                id="ds-params"
                className="font-mono"
                value={form.params}
                placeholder={t("db.paramsPlaceholder")}
                onChange={(e) => set("params", e.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="ds-note">{t("db.note")}</FieldLabel>
              <Input
                id="ds-note"
                value={form.note}
                onChange={(e) => set("note", e.target.value)}
              />
            </Field>
            <div className="flex items-center gap-2">
              <Switch
                id="ds-readonly"
                checked={form.readOnly}
                onCheckedChange={(v) => set("readOnly", v)}
              />
              <FieldLabel htmlFor="ds-readonly">{t("db.readOnly")}</FieldLabel>
            </div>
            <p className="text-xs text-muted-foreground">
              {t("db.readOnlyHint")}
            </p>

            <div className="flex items-center gap-2">
              <Switch
                id="ds-enabled"
                checked={!form.disabled}
                onCheckedChange={(v) => set("disabled", !v)}
              />
              <FieldLabel htmlFor="ds-enabled">{t("db.enabled")}</FieldLabel>
            </div>
            <p className="text-xs text-muted-foreground">
              {t("db.disabledHint")}
            </p>
          </FieldGroup>
        </TabsContent>
      </Tabs>

      <TestResult state={test} />

      <DialogFooter className="gap-2 sm:justify-between">
        <div className="flex gap-2">
          {/* URI 按钮的位置照 Navicat：连接对话框左下角。 */}
          <Button
            type="button"
            variant="outline"
            onClick={() => setUriOpen(true)}
          >
            {t("db.uriButton")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={saving || test.status === "running"}
            onClick={handleTest}
          >
            {test.status === "running" ? <Spinner /> : null}
            {test.status === "running" ? t("db.testing") : t("db.test")}
          </Button>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" disabled={saving}>
            {saving ? <Spinner /> : null}
            {t("common.save")}
          </Button>
        </div>
      </DialogFooter>

      <UriDialog
        open={uriOpen}
        sourceId={source?.id ?? 0}
        onOpenChange={setUriOpen}
        onImport={(parsed) => {
          setForm((prev) => ({ ...prev, ...parsed }))
          setTest({ status: "idle" })
        }}
      />
    </form>
  )
}

/**
 * 库选择器：点开时现连一次库把清单拉回来。
 *
 * 不做成自由输入是因为库名敲错要等到真去查表才报错，而那时人已经忘了
 * 自己敲了什么；列表选还能顺便看到每个库有多少张表。
 */
function DatabasePicker({
  sourceId,
  form,
  onPick,
}: {
  sourceId: number
  form: DataSourceInput
  onPick: (name: string) => void
}) {
  const { t } = useTranslation()
  const [list, setList] = useState<DbDatabase[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function load() {
    setLoading(true)
    setError(null)
    try {
      setList(await api.datasources.probeDatabases({ ...form, id: sourceId }))
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <Field>
      <FieldLabel htmlFor="ds-database">{t("db.database")}</FieldLabel>
      <div className="flex items-center gap-2">
        <Select
          value={form.database || ""}
          onValueChange={(v) => onPick(v ?? "")}
        >
          <SelectTrigger id="ds-database" className="flex-1">
            <SelectValue placeholder={t("db.databasePick")} />
          </SelectTrigger>
          <SelectContent>
            {/* 已选的库先摆上，免得还没拉清单时显示成空。 */}
            {(list ?? (form.database ? [{ name: form.database, tables: 0 }] : []))
              .map((d) => (
                <SelectItem key={d.name} value={d.name}>
                  {d.name}
                </SelectItem>
              ))}
          </SelectContent>
        </Select>
        <Button type="button" variant="outline" size="sm" disabled={loading} onClick={load}>
          {loading ? <Spinner /> : null}
          {t("db.databaseLoad")}
        </Button>
      </div>
      {error ? (
        <p className="text-xs text-destructive">{error}</p>
      ) : (
        <p className="text-xs text-muted-foreground">{t("db.databaseHint")}</p>
      )}
    </Field>
  )
}

function TestResult({ state }: { state: TestState }) {
  const { t } = useTranslation()
  if (state.status === "ok") {
    return (
      <p className="flex items-center gap-1.5 text-xs text-success">
        <CheckCircle2Icon className="size-3.5 shrink-0" />
        {t("db.testOk", { version: state.version ?? "" })}
      </p>
    )
  }
  if (state.status === "failed") {
    return (
      <p className="flex items-start gap-1.5 text-xs text-destructive">
        <XCircleIcon className="mt-0.5 size-3.5 shrink-0" />
        <span className="min-w-0 break-all">
          {t("db.testFailed")}
          {state.error ? `：${state.error}` : null}
        </span>
      </p>
    )
  }
  return null
}
