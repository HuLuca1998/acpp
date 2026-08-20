import { useTranslation } from "react-i18next"

import { isRequired } from "@/lib/mcp-tool"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import type { McpInputSchema, McpSchemaProperty } from "@/types/acp"

/**
 * 按工具的 inputSchema 渲染参数控件。
 *
 * 每个参数的 label 是参数名本身（等宽），说明用 schema 里那句原文——
 * 那正是模型读到的东西。人和模型看同一份说明，才谈得上「复现 AI 的调用」。
 *
 * 取值一律以字符串收着，发送前才按类型转：中间态（正在输入的数字、
 * 清空后的字段）用字符串表达最直接，转换只发生在出口一处。
 */

/**
 * 用多行输入框的参数名。JSON Schema 没有「这一栏该多行」这种标记，
 * 只能按名字认——SQL 挤在单行输入框里根本没法读，这点体验值得一条名单。
 */
const MULTILINE = new Set(["sql", "query", "body", "content", "text"])

export function ToolArgForm({
  schema,
  values,
  onChange,
}: {
  schema?: McpInputSchema
  values: Record<string, string>
  onChange: (name: string, value: string) => void
}) {
  const { t } = useTranslation()
  const properties = Object.entries(schema?.properties ?? {})

  if (properties.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        {t("tools.form.noParams")}
      </p>
    )
  }

  return (
    <FieldGroup className="gap-4">
      {properties.map(([name, prop]) => (
        <Field key={name}>
          <FieldLabel htmlFor={`arg-${name}`} className="font-mono text-xs">
            {name}
            {isRequired(schema, name) ? (
              <span className="text-destructive">*</span>
            ) : null}
            <span className="font-sans text-[11px] font-normal text-muted-foreground">
              {prop.type ?? "any"}
            </span>
          </FieldLabel>
          <ArgControl
            name={name}
            prop={prop}
            value={values[name] ?? ""}
            onChange={(v) => onChange(name, v)}
          />
          {prop.description ? (
            <FieldDescription>{prop.description}</FieldDescription>
          ) : null}
        </Field>
      ))}
    </FieldGroup>
  )
}

function ArgControl({
  name,
  prop,
  value,
  onChange,
}: {
  name: string
  prop: McpSchemaProperty
  value: string
  onChange: (value: string) => void
}) {
  const id = `arg-${name}`

  if (prop.enum && prop.enum.length > 0) {
    return (
      <Select value={value} onValueChange={(v) => onChange(v ?? "")}>
        <SelectTrigger id={id} className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {prop.enum.map((option) => (
            <SelectItem key={option} value={option}>
              {option}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    )
  }

  if (prop.type === "boolean") {
    return (
      <Switch
        id={id}
        checked={value === "true"}
        onCheckedChange={(on) => onChange(on ? "true" : "false")}
      />
    )
  }

  if (prop.type === "number" || prop.type === "integer") {
    return (
      <Input
        id={id}
        type="number"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="font-mono text-sm"
      />
    )
  }

  if (MULTILINE.has(name)) {
    return (
      <Textarea
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        rows={4}
        className="font-mono text-sm"
        spellCheck={false}
      />
    )
  }

  return (
    <Input
      id={id}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="font-mono text-sm"
    />
  )
}
