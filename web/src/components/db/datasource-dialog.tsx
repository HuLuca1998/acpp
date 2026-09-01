import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { api, ApiError } from "@/lib/api"
import { copyText } from "@/lib/clipboard"
import type {
  DataSource,
  DataSourceInput,
  DbDatabase,
  Server,
  ServerInput,
} from "@/types/acp"
import { Hint } from "@/components/hint"
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
import {
  Combobox,
  ComboboxContent,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from "@/components/ui/combobox"
import { UriDialog } from "@/components/db/uri-dialog"
import { ServerDialog } from "@/components/servers/server-dialog"
import { TestResult, type TestState } from "@/components/connection-test"
import { useAsyncData } from "@/hooks/use-async-data"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Spinner } from "@/components/ui/spinner"
import { CopyIcon, PlusIcon } from "lucide-react"

/** URI 里带 SSH 却没带私钥路径时的缺省——本机约定私钥放这。 */
const DEFAULT_SSH_KEY_PATH = "~/.ssh/key"

// 与 `openssl rand -base64 48 | tr -dc 'A-Za-z0-9' | head -c 32` 同款：
// 62 字符字母数字表取 32 位（约 190 bit 熵），拒绝采样保证均匀。
const PASSWORD_ALPHABET =
  "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

function generatePassword(length = 32): string {
  const out: string[] = []
  while (out.length < length) {
    for (const b of crypto.getRandomValues(new Uint8Array(length * 2))) {
      if (b >= 248) continue // 248 = 62*4，再往上取模就不均匀了
      out.push(PASSWORD_ALPHABET[b % PASSWORD_ALPHABET.length])
      if (out.length === length) break
    }
  }
  return out.join("")
}

function readonlyUserSQL(password: string): string {
  return [
    `CREATE USER 'readonly'@'%' IDENTIFIED BY '${password}';`,
    `GRANT SELECT, SHOW VIEW ON *.* TO 'readonly'@'%';`,
  ].join("\n")
}

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
    user: source?.user ?? "readonly",
    password: "",
    database: source?.database ?? "",
    params: source?.params ?? "",
    note: source?.note ?? "",
    sshEnabled: source?.sshEnabled ?? false,
    serverId: source?.serverId ?? 0,
    readOnly: source?.readOnly ?? true,
    disabled: source?.disabled ?? false,
  }))
  const [saving, setSaving] = useState(false)
  // 只读建号语句里现场生成的密码，对话框打开期间保持不变。
  const [genPassword] = useState(generatePassword)
  const [test, setTest] = useState<TestState>({ status: "idle" })
  const [sshTest, setSSHTest] = useState<TestState>({ status: "idle" })
  const [uriOpen, setUriOpen] = useState(false)
  // 服务器清单：跳板机从这里选（adr-019）。新建一台之后 bump 一下重新拉，
  // 免得刚建好的那台要关掉对话框才看得见。
  const [serversVersion, setServersVersion] = useState(0)
  const { data: servers } = useAsyncData(
    () =>
      api.servers
        .list()
        .then((res) => res.items.filter((srv) => !srv.disabled))
        .catch(() => [] as Server[]),
    [serversVersion]
  )
  const [serverDialogOpen, setServerDialogOpen] = useState(false)
  // URI 里带着跳板机信息时的线索：它描述的机器未必已经配过，所以不直接
  // 填进表单，而是提示用户照它新建一台（按主机名去猜某条已存记录，猜错
  // 就连到别的机器上去了）。
  const [sshHint, setSSHHint] = useState<ServerInput | null>(null)

  const set = <K extends keyof DataSourceInput>(
    key: K,
    value: DataSourceInput[K]
  ) => {
    setForm((prev) => ({ ...prev, [key]: value }))
    // 改了任何字段，上一次的测试结论就作废了。
    setTest({ status: "idle" })
    setSSHTest({ status: "idle" })
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

  // 复制建号 SQL 时把生成的密码顺手填进密码框——库里建的号和这条连接存
  // 的密码天然一致。密码框已有内容（手敲过 / 编辑态的「留空=不改」）就不
  // 碰，只复制。
  async function copyReadonlySQL() {
    if (!(await copyText(readonlyUserSQL(genPassword)))) {
      toast.error(t("common.copyFailed"))
      return
    }
    if (!form.password) {
      set("password", genPassword)
      toast.success(t("db.readonlyUserCopied"))
    } else {
      toast.success(t("db.readonlyUserCopiedKept"))
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

  // SSH 单独测走 probe 模式（同选库的「读取」）：表单直接测、不落库，
  // 编辑时带 id 让后端沿用已存密码——隧道和库两层分开测，报错才知道卡在哪层。
  async function handleTestSSH() {
    setSSHTest({ status: "running" })
    try {
      const result = await api.datasources.probeSSH({
        ...form,
        id: source?.id ?? 0,
      })
      setSSHTest(
        result.ok
          ? { status: "ok", version: result.version }
          : { status: "failed", error: result.error ?? "" }
      )
    } catch (err) {
      setSSHTest({ status: "failed", error: (err as Error).message })
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
                {/* 建议而非限制：库常常先于代码存在，本机还没 clone 也得能配。
                    inputValue 与 value 都接同一个字段，输入什么就是什么，
                    选建议只是替你把字打完。 */}
                <Combobox
                  items={projects}
                  value={form.project}
                  onValueChange={(v) => set("project", (v as string) ?? "")}
                  inputValue={form.project}
                  onInputValueChange={(v) => set("project", v)}
                  openOnInputClick
                >
                  <ComboboxInput
                    id="ds-project"
                    required
                    placeholder={t("db.projectPlaceholder")}
                    showTrigger={projects.length > 0}
                  />
                  <ComboboxContent>
                    <ComboboxList>
                      {(project: string) => (
                        <ComboboxItem
                          key={project}
                          value={project}
                          className="font-mono"
                        >
                          {project}
                        </ComboboxItem>
                      )}
                    </ComboboxList>
                  </ComboboxContent>
                </Combobox>
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

            {/* 只读建号引导：软件层的只读开关挡不住存储过程/动态 SQL（高级
                页签里说了），真正的边界是账号授权——所以新建默认 readonly，
                并把可直接执行的建号语句摆在手边。 */}
            <Field>
              <p className="text-xs text-muted-foreground">
                {t("db.readonlyUserHint")}
              </p>
              <div className="flex items-start gap-2">
                <code className="min-w-0 flex-1 rounded-lg border border-border bg-muted/40 px-2.5 py-2 font-mono text-xs leading-5 break-all whitespace-pre-wrap">
                  {readonlyUserSQL(genPassword)}
                </code>
                <Hint label={t("common.copy")} align="end">
                  <Button
                    type="button"
                    variant="outline"
                    size="icon"
                    aria-label={t("common.copy")}
                    onClick={() => void copyReadonlySQL()}
                  >
                    <CopyIcon />
                  </Button>
                </Hint>
              </div>
            </Field>

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
                  {t("server.pickHint")}
                </p>

                {/* 跳板机是服务器页的一条记录（adr-019）：同一台机器既是
                    这条隧道的入口，也是 AI 观察的对象，配一次两边都用。 */}
                <Field>
                  <FieldLabel htmlFor="ds-server">
                    {t("server.pickForTunnel")}
                  </FieldLabel>
                  <div className="flex items-center gap-2">
                    <Select
                      value={form.serverId ? String(form.serverId) : ""}
                      onValueChange={(v) => set("serverId", Number(v))}
                    >
                      <SelectTrigger id="ds-server" className="flex-1">
                        <SelectValue
                          placeholder={
                            servers && servers.length === 0
                              ? t("server.pickEmpty")
                              : t("server.pickPlaceholder")
                          }
                        />
                      </SelectTrigger>
                      <SelectContent>
                        {(servers ?? []).map((srv: Server) => (
                          <SelectItem key={srv.id} value={String(srv.id)}>
                            <span className="font-mono">{srv.name}</span>
                            <span className="ml-2 text-muted-foreground">
                              {srv.user}@{srv.host}:{srv.port}
                            </span>
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <Hint label={t("server.pickCreate")} align="end">
                      <Button
                        type="button"
                        variant="outline"
                        size="icon"
                        aria-label={t("server.pickCreate")}
                        onClick={() => setServerDialogOpen(true)}
                      >
                        <PlusIcon />
                      </Button>
                    </Hint>
                  </div>
                </Field>

                {/* 从 URI 导进来的跳板机：一键照它建一台，省去回头手抄。 */}
                {sshHint ? (
                  <div className="flex items-center justify-between gap-2 rounded-md border border-dashed px-3 py-2">
                    <span className="min-w-0 truncate text-xs text-muted-foreground">
                      {t("db.uriSSHHint", {
                        host: `${sshHint.user}@${sshHint.host}:${sshHint.port}`,
                      })}
                    </span>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => setServerDialogOpen(true)}
                    >
                      {t("server.pickCreate")}
                    </Button>
                  </div>
                ) : null}

                <div className="flex items-center gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={sshTest.status === "running" || !form.serverId}
                    onClick={handleTestSSH}
                  >
                    {sshTest.status === "running" ? <Spinner /> : null}
                    {sshTest.status === "running"
                      ? t("db.testing")
                      : t("db.sshTest")}
                  </Button>
                </div>
                <TestResult state={sshTest} variant="ssh" />

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
          const { sshHint: hint, ...fields } = parsed
          setForm((prev) => ({ ...prev, ...fields }))
          // Navicat 的 URI 通常不带私钥路径，补上本机缺省，免得每次导入
          // 都手填一遍。跳板机本身不进表单——它是服务器页的记录。
          setSSHHint(
            hint
              ? { ...hint, keyPath: hint.keyPath || DEFAULT_SSH_KEY_PATH }
              : null
          )
          setTest({ status: "idle" })
          setSSHTest({ status: "idle" })
        }}
      />

      {/* 新建跳板机：建完自动选中它——用户点这个按钮就是为了用它。 */}
      <ServerDialog
        open={serverDialogOpen}
        onOpenChange={setServerDialogOpen}
        server={null}
        prefill={sshHint}
        onSaved={(saved) => {
          setServersVersion((v) => v + 1)
          set("serverId", saved.id)
          setSSHHint(null)
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
            {(
              list ??
              (form.database ? [{ name: form.database, tables: 0 }] : [])
            ).map((d) => (
              <SelectItem key={d.name} value={d.name}>
                {d.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={loading}
          onClick={load}
        >
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

