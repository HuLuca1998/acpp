import type { ElicitationQuestion } from "@/types/acp"

/** elicitation requestedSchema（JSON Schema object）的最小形状。 */
export interface ElicitationSchema {
  properties?: Record<
    string,
    {
      title?: string
      description?: string
      oneOf?: { const?: string; description?: string }[]
      _meta?: { codex?: { isOtherAnswer?: boolean; questionId?: string } }
    }
  >
  required?: string[]
}

const CUSTOM_SUFFIX = "_custom"

/** claude 的自由输入字段：`<题目id>_custom` 命名约定，且对应题目存在。 */
function claudeCustomTarget(
  key: string,
  props: NonNullable<ElicitationSchema["properties"]>
): string | null {
  if (!key.endsWith(CUSTOM_SUFFIX)) return null
  const target = key.slice(0, -CUSTOM_SUFFIX.length)
  return target in props ? target : null
}

/**
 * 把 requestedSchema 解析成结构化题目列表，自由输入字段归位为对应题的
 * otherFieldId。两条 ACP 的标记方式不同：codex 用 `_meta.codex.isOtherAnswer`
 * （字段名 `__other`），claude 用 `<题目id>_custom` 命名约定——差异吃在这里。
 */
export function parseElicitationSchema(
  schema: ElicitationSchema | null | undefined
): ElicitationQuestion[] {
  const props = schema?.properties ?? {}
  const required = new Set(schema?.required ?? [])

  const others = new Map<string, string>()
  for (const [key, prop] of Object.entries(props)) {
    const codex = prop._meta?.codex
    if (codex?.isOtherAnswer && codex.questionId) {
      others.set(codex.questionId, key)
      continue
    }
    const target = claudeCustomTarget(key, props)
    if (target) others.set(target, key)
  }

  const questions: ElicitationQuestion[] = []
  for (const [key, prop] of Object.entries(props)) {
    if (prop._meta?.codex?.isOtherAnswer) continue
    if (claudeCustomTarget(key, props)) continue
    questions.push({
      id: key,
      title: prop.title ?? key,
      description: prop.description,
      required: required.has(key),
      options: (prop.oneOf ?? []).flatMap((o) =>
        o.const ? [{ value: o.const, description: o.description }] : []
      ),
      otherFieldId: others.get(key),
    })
  }
  return questions
}

/** 从作答记录里取某道题的答案：优先自由输入，其次选项。 */
export function answerFor(
  question: ElicitationQuestion,
  answers: Record<string, unknown> | null | undefined
): string {
  if (!answers) return ""
  if (question.otherFieldId) {
    const other = answers[question.otherFieldId]
    if (typeof other === "string" && other !== "") return other
  }
  const value = answers[question.id]
  return typeof value === "string" ? value : ""
}
