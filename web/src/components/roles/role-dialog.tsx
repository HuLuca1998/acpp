import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { Agent, Role, RoleInput } from "@/types/acp"
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
import { Textarea } from "@/components/ui/textarea"

/** 未选择时 Select 的占位值（Base UI Select 不接受空串 item）。 */
const NONE = "__none"

/**
 * 角色编辑对话框：新建（role 为 null）与编辑共用。表单状态由 key 驱动
 * 重挂初始化（无 effect 同步）；模型/思考深度/权限档选项来自所选工具的
 * 探测缓存，空值 = 沿用该工具默认档。
 */
export function RoleDialog({
  open,
  role,
  agents,
  onClose,
  onSaved,
}: {
  open: boolean
  role: Role | null
  agents: Agent[]
  onClose: () => void
  onSaved: (role: Role) => void
}) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {role ? t("roles.editTitle") : t("roles.addTitle")}
          </DialogTitle>
          <DialogDescription>{t("roles.dialogHint")}</DialogDescription>
        </DialogHeader>
        {open ? (
          <RoleForm
            key={role?.id ?? "new"}
            role={role}
            agents={agents}
            onClose={onClose}
            onSaved={onSaved}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

function RoleForm({
  role,
  agents,
  onClose,
  onSaved,
}: {
  role: Role | null
  agents: Agent[]
  onClose: () => void
  onSaved: (role: Role) => void
}) {
  const { t } = useTranslation()
  const [form, setForm] = useState<RoleInput>(() =>
    role
      ? {
          name: role.name,
          description: role.description,
          persona: role.persona,
          agentId: role.agentId,
          model: role.model,
          effort: role.effort,
          level: role.level,
        }
      : {
          name: "",
          description: "",
          persona: "",
          agentId: agents[0]?.id ?? 0,
          model: "",
          effort: "",
          level: "",
        }
  )
  const [saving, setSaving] = useState(false)

  const agent = useMemo(
    () => agents.find((a) => a.id === form.agentId),
    [agents, form.agentId]
  )
  const models = agent?.models?.filter((m) => !m.disabled) ?? []
  const efforts = agent?.skeleton?.efforts ?? []
  const levels = agent?.skeleton?.levels ?? []

  function patch(p: Partial<RoleInput>) {
    setForm((prev) => ({ ...prev, ...p }))
  }

  async function save() {
    setSaving(true)
    try {
      const saved = role
        ? await api.roles.update(role.id, form)
        : await api.roles.create(form)
      toast.success(t(role ? "roles.updated" : "roles.created"))
      onSaved(saved)
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
          <FieldLabel htmlFor="role-name">{t("roles.name")}</FieldLabel>
          <Input
            id="role-name"
            value={form.name}
            onChange={(e) => patch({ name: e.target.value })}
            placeholder={t("roles.namePlaceholder")}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="role-desc">
            {t("roles.colDescription")}
          </FieldLabel>
          <Textarea
            id="role-desc"
            value={form.description}
            onChange={(e) => patch({ description: e.target.value })}
            placeholder={t("roles.descPlaceholder")}
            rows={2}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="role-persona">{t("roles.persona")}</FieldLabel>
          <Textarea
            id="role-persona"
            value={form.persona}
            onChange={(e) => patch({ persona: e.target.value })}
            placeholder={t("roles.personaPlaceholder")}
            rows={5}
          />
        </Field>
        <Field>
          <FieldLabel>{t("roles.agent")}</FieldLabel>
          <Select
            value={String(form.agentId || "")}
            onValueChange={(v) =>
              patch({ agentId: Number(v), model: "", effort: "", level: "" })
            }
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {agents.map((a) => (
                <SelectItem key={a.id} value={String(a.id)}>
                  {a.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <Field>
            <FieldLabel>{t("roles.model")}</FieldLabel>
            <Select
              value={form.model || NONE}
              onValueChange={(v) => patch({ model: !v || v === NONE ? "" : v })}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NONE}>{t("roles.default")}</SelectItem>
                {models.map((m) => (
                  <SelectItem key={m.id} value={m.id}>
                    {m.alias || m.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel>{t("roles.effort")}</FieldLabel>
            <Select
              value={form.effort || NONE}
              onValueChange={(v) =>
                patch({ effort: !v || v === NONE ? "" : v })
              }
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NONE}>{t("roles.default")}</SelectItem>
                {efforts.map((e) => (
                  <SelectItem key={e} value={e}>
                    {t(`chat.settings.effort.${e}` as never, {
                      defaultValue: e,
                    })}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel>{t("roles.level")}</FieldLabel>
            <Select
              value={form.level || NONE}
              onValueChange={(v) => patch({ level: !v || v === NONE ? "" : v })}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NONE}>{t("roles.default")}</SelectItem>
                {levels.map((l) => (
                  <SelectItem key={l} value={l}>
                    {t(`chat.settings.level.${l}` as never, {
                      defaultValue: l,
                    })}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        </div>
      </FieldGroup>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          {t("common.cancel")}
        </Button>
        <Button
          onClick={() => void save()}
          disabled={saving || !form.name.trim() || !form.agentId}
        >
          {t("common.save")}
        </Button>
      </DialogFooter>
    </>
  )
}
