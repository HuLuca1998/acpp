import type { ElicitationQuestion } from "@/types/acp"

/**
 * elicitation requestedSchema（JSON Schema object）的最小形状。
 * 2026-08 从真实转录核对：单选是顶层 oneOf；多选是 `type:"array"` +
 * `items.anyOf`（oneOf/enum 一并兜底）；自由输入字段有三种标记。
 */
export interface ElicitationSchema {
  properties?: Record<
    string,
    {
      type?: string
      title?: string
      description?: string
      oneOf?: { const?: string; description?: string }[]
      items?: {
        anyOf?: { const?: string; description?: string }[]
        oneOf?: { const?: string; description?: string }[]
        enum?: string[]
      }
      _meta?: {
        codex?: { isOtherAnswer?: boolean; questionId?: string }
        _askUserQuestionCustomAnswer?: {
          isCustomAnswer?: boolean
          questionId?: string
        }
      }
    }
  >
  required?: string[]
}

const CUSTOM_SUFFIX = "_custom"

type SchemaProps = NonNullable<ElicitationSchema["properties"]>

/**
 * 自由输入字段的归属题目：claude 新版走 `_askUserQuestionCustomAnswer`
 * 标记，codex 走 `_meta.codex.isOtherAnswer`，claude 旧版靠
 * `<题目id>_custom` 命名约定（且对应题目存在）。
 */
function otherTarget(key: string, props: SchemaProps): string | null {
  const prop = props[key]
  const codex = prop._meta?.codex
  if (codex?.isOtherAnswer) return codex.questionId ?? null
  const ask = prop._meta?._askUserQuestionCustomAnswer
  if (ask?.isCustomAnswer) return ask.questionId ?? null
  if (!key.endsWith(CUSTOM_SUFFIX)) return null
  const target = key.slice(0, -CUSTOM_SUFFIX.length)
  return target in props ? target : null
}

/** 把 requestedSchema 解析成结构化题目列表，自由输入字段归位到对应题。 */
export function parseElicitationSchema(
  schema: ElicitationSchema | null | undefined
): ElicitationQuestion[] {
  const props = schema?.properties ?? {}
  const required = new Set(schema?.required ?? [])

  const others = new Map<string, string>()
  for (const key of Object.keys(props)) {
    const target = otherTarget(key, props)
    if (target) others.set(target, key)
  }

  const questions: ElicitationQuestion[] = []
  for (const [key, prop] of Object.entries(props)) {
    if (otherTarget(key, props) !== null) continue
    const multiple = prop.type === "array"
    const source = multiple
      ? [
          ...(prop.items?.anyOf ?? []),
          ...(prop.items?.oneOf ?? []),
          ...(prop.items?.enum ?? []).map((e) => ({ const: e })),
        ]
      : (prop.oneOf ?? [])
    questions.push({
      id: key,
      title: prop.title ?? key,
      description: prop.description,
      required: required.has(key),
      multiple,
      options: source.flatMap((o) =>
        o.const
          ? [
              {
                value: o.const,
                description: "description" in o ? o.description : undefined,
              },
            ]
          : []
      ),
      otherFieldId: others.get(key),
    })
  }
  return questions
}

/**
 * 从作答记录里取某道题的答案集合：题目字段（单值或数组）与自由输入字段
 * 合并——多选题两者可以并存。
 */
export function answersFor(
  question: ElicitationQuestion,
  answers: Record<string, unknown> | null | undefined
): string[] {
  if (!answers) return []
  const out: string[] = []
  const value = answers[question.id]
  if (typeof value === "string" && value !== "") out.push(value)
  if (Array.isArray(value)) {
    for (const v of value) if (typeof v === "string" && v !== "") out.push(v)
  }
  if (question.otherFieldId) {
    const other = answers[question.otherFieldId]
    if (typeof other === "string" && other !== "") out.push(other)
  }
  return out
}
