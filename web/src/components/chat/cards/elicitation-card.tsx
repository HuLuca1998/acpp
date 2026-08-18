import { useMemo, useState, type FormEvent, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

import type { Message, PendingElicitation } from "@/types/acp"
import { cn } from "@/lib/utils"
import {
  answerFor,
  parseElicitationSchema,
  type ElicitationSchema,
} from "@/lib/elicitation"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Item,
  ItemContent,
  ItemDescription,
  ItemGroup,
  ItemMedia,
  ItemTitle,
} from "@/components/ui/item"
import {
  Questionnaire,
  QuestionnaireActions,
  QuestionnaireChoice,
  QuestionnaireChoiceDescription,
  QuestionnaireChoices,
  QuestionnaireDescription,
  QuestionnaireInput,
  QuestionnaireItem,
  QuestionnaireNext,
  QuestionnairePrevious,
  QuestionnaireProgress,
  QuestionnaireSkip,
  QuestionnaireSubmit,
  QuestionnaireTitle,
} from "@/components/ui/questionnaire"
import { CheckIcon, CircleHelpIcon } from "lucide-react"

/**
 * agent 的交互式提问卡片，用 shadcn 的 Questionnaire 原语搭：一次一题，
 * 选项与自由输入共用题目 id 作字段名——**互斥由原语保证**（选选项会摘掉
 * 输入框的 name，反之亦然），我们只管在提交时按值判断该写回哪个字段。
 *
 * 提交走 accept，整份不答走 decline；单题的「跳过」是原语自带的三态之一
 * （未答 / 已答 / 已跳过），跳过的题不会出现在表单值里。
 */
export function ElicitationCard({
  elicitation,
  onResolve,
}: {
  elicitation: PendingElicitation
  onResolve: (
    action: "accept" | "decline",
    content?: Record<string, string>
  ) => void
}) {
  const { t } = useTranslation()
  const [submitted, setSubmitted] = useState(false)
  const questions = elicitation.questions

  // 把整份问卷声明给 Root：它据此分配数字快捷键，并在开发期核对渲染出来的
  // 题目与选项跟声明一致（漏一个选项会在控制台点名）。
  const items = useMemo(
    () =>
      questions.map((q) => ({
        name: q.id,
        required: q.required,
        choices: q.options.map((o) => ({ value: o.value })),
      })),
    [questions]
  )

  function decline() {
    if (submitted) return
    setSubmitted(true)
    onResolve("decline")
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (submitted) return

    const data = new FormData(event.currentTarget)
    const content: Record<string, string> = {}
    for (const q of questions) {
      const value = data.get(q.id)
      if (typeof value !== "string" || value.trim() === "") continue
      // 自由输入与选项同名，靠「是不是某个选项的值」区分该写回哪个字段：
      // 纯自由输入题没有独立的 other 字段，答案直接写回题目本身。
      const isOption = q.options.some((o) => o.value === value)
      content[isOption ? q.id : (q.otherFieldId ?? q.id)] = value
    }

    setSubmitted(true)
    // 一题没答就别拿空答案糊弄 agent，按「不回答」处理。
    if (Object.keys(content).length === 0) {
      onResolve("decline")
      return
    }
    onResolve("accept", content)
  }

  // 解析不出题目时（schema 为空或形状不认识）退回纯文本 + 两个按钮，
  // 至少别把 agent 卡在那儿等一个永远点不出来的答复。
  if (questions.length === 0) {
    return (
      <ElicitationShell title={t("chat.elicitation.title")}>
        <div className="text-sm text-muted-foreground">
          {elicitation.message}
        </div>
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            disabled={submitted}
            onClick={() => {
              setSubmitted(true)
              onResolve("accept", {})
            }}
          >
            {t("chat.elicitation.submit")}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="ml-auto text-muted-foreground"
            disabled={submitted}
            onClick={decline}
          >
            {t("chat.elicitation.dismiss")}
          </Button>
        </div>
      </ElicitationShell>
    )
  }

  return (
    <Questionnaire
      items={items}
      shortcuts="numbers"
      inert={submitted}
      className="rounded-xl border border-border bg-card p-4 shadow-sm"
      onSubmit={handleSubmit}
    >
      <ElicitationHeader title={t("chat.elicitation.title")}>
        <QuestionnaireProgress
          className="ml-auto"
          render={(props, state) =>
            // 只有一题时进度条是废话，不渲染。
            state.total > 1 ? (
              <div {...props}>
                {t("chat.elicitation.progress", {
                  current: state.current,
                  total: state.total,
                })}
              </div>
            ) : null
          }
        />
        <Button
          type="button"
          size="sm"
          variant="ghost"
          className="-my-1 text-muted-foreground"
          disabled={submitted}
          onClick={decline}
        >
          {t("chat.elicitation.dismiss")}
        </Button>
      </ElicitationHeader>

      {questions.map((q) => (
        <QuestionnaireItem key={q.id} name={q.id} required={q.required}>
          <QuestionnaireTitle>{q.title}</QuestionnaireTitle>
          {q.description ? (
            <QuestionnaireDescription>{q.description}</QuestionnaireDescription>
          ) : null}

          {q.options.length > 0 ? (
            <QuestionnaireChoices>
              {q.options.map((option) => (
                <QuestionnaireChoice key={option.value} value={option.value}>
                  {option.value}
                  {option.description ? (
                    <QuestionnaireChoiceDescription>
                      {option.description}
                    </QuestionnaireChoiceDescription>
                  ) : null}
                </QuestionnaireChoice>
              ))}
            </QuestionnaireChoices>
          ) : null}

          {q.otherFieldId || q.options.length === 0 ? (
            <QuestionnaireInput
              placeholder={t("chat.elicitation.otherPlaceholder")}
            />
          ) : null}
        </QuestionnaireItem>
      ))}

      <QuestionnaireActions>
        <QuestionnairePrevious size="sm" variant="ghost">
          {t("chat.elicitation.back")}
        </QuestionnairePrevious>
        <QuestionnaireSkip size="sm" variant="ghost">
          {t("chat.elicitation.skip")}
        </QuestionnaireSkip>
        <QuestionnaireNext size="sm">
          {t("chat.elicitation.next")}
        </QuestionnaireNext>
        <QuestionnaireSubmit size="sm">
          {t("chat.elicitation.submit")}
        </QuestionnaireSubmit>
      </QuestionnaireActions>
    </Questionnaire>
  )
}

/** 已完成的交互式提问：每题列出全部选项并标出用户的选择，可随时回看。 */
export function ElicitationAnsweredCard({ message }: { message: Message }) {
  const { t } = useTranslation()
  const payload = message.payload as {
    action?: string
    schema?: unknown
    answers?: Record<string, unknown>
  } | null

  const questions = parseElicitationSchema(
    (payload?.schema ?? null) as ElicitationSchema | null
  )
  const accepted = payload?.action === "accept"

  return (
    <ElicitationShell
      title={t("chat.elicitation.title")}
      badge={
        !accepted ? (
          <Badge variant="secondary" className="ml-auto">
            {t("chat.elicitation.skipped")}
          </Badge>
        ) : null
      }
    >
      {questions.length === 0 ? (
        <div className="text-sm text-muted-foreground">{message.content}</div>
      ) : (
        questions.map((q) => {
          const answer = answerFor(q, payload?.answers)
          // 答案不在选项里 = 用户自己填的，补一行显示出来。
          const custom =
            answer !== "" && !q.options.some((o) => o.value === answer)
          return (
            <div key={q.id} className="flex flex-col gap-2">
              <div>
                <div className="text-sm font-medium">{q.title}</div>
                {q.description ? (
                  <div className="text-sm text-muted-foreground">
                    {q.description}
                  </div>
                ) : null}
              </div>
              <ItemGroup className="gap-2">
                {q.options.map((option) => (
                  <AnsweredChoice
                    key={option.value}
                    label={option.value}
                    description={option.description}
                    picked={option.value === answer}
                  />
                ))}
                {custom ? <AnsweredChoice label={answer} picked /> : null}
                {answer === "" ? (
                  <p className="text-sm text-muted-foreground">
                    {t("chat.elicitation.skipped")}
                  </p>
                ) : null}
              </ItemGroup>
            </div>
          )
        })
      )}
    </ElicitationShell>
  )
}

/** 回看态的一行选项：形状照 QuestionnaireChoice，只是不可交互。 */
function AnsweredChoice({
  label,
  description,
  picked,
}: {
  label: string
  description?: string
  picked: boolean
}) {
  return (
    <Item
      variant="outline"
      size="sm"
      className={cn(
        picked ? "border-primary/40 bg-muted" : "text-muted-foreground/70"
      )}
    >
      <ItemMedia
        variant="icon"
        className={cn(
          "size-4 self-start rounded-full border",
          picked
            ? "border-primary bg-primary text-primary-foreground"
            : "border-input"
        )}
      >
        {picked ? <CheckIcon className="size-3" /> : null}
      </ItemMedia>
      <ItemContent className="gap-0.5">
        <ItemTitle className="font-normal">{label}</ItemTitle>
        {description ? <ItemDescription>{description}</ItemDescription> : null}
      </ItemContent>
    </Item>
  )
}

function ElicitationShell({
  title,
  badge,
  children,
}: {
  title: string
  badge?: ReactNode
  children: ReactNode
}) {
  return (
    <div className="flex flex-col gap-4 rounded-xl border border-border bg-card p-4 shadow-sm">
      <ElicitationHeader title={title}>{badge}</ElicitationHeader>
      {children}
    </div>
  )
}

function ElicitationHeader({
  title,
  children,
}: {
  title: string
  children?: ReactNode
}) {
  return (
    <div className="flex items-center gap-2 text-sm font-medium">
      <CircleHelpIcon className="size-4 text-primary" />
      {title}
      {children}
    </div>
  )
}
