import { useState } from "react"
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
import { Input } from "@/components/ui/input"
import { CircleHelpIcon } from "lucide-react"

/**
 * agent 的交互式提问卡片：每道题渲染选项按钮组，可附自由输入；
 * 选选项和填「其他」互斥。提交走 accept，跳过走 decline。
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
  const [answers, setAnswers] = useState<Record<string, string>>({})
  const [others, setOthers] = useState<Record<string, string>>({})
  const [step, setStep] = useState(0)
  const [submitted, setSubmitted] = useState(false)

  type Answers = Record<string, string>

  const questions = elicitation.questions
  const total = questions.length
  const q = questions[Math.min(step, total - 1)]
  const isLast = step >= total - 1

  const isAnswered = (
    question: PendingElicitation["questions"][number],
    a: Answers,
    o: Answers
  ) => Boolean(a[question.id]) || (o[question.id]?.trim() ?? "") !== ""

  const isComplete = (a: Answers, o: Answers) =>
    questions.some((question) => isAnswered(question, a, o)) &&
    questions.every(
      (question) => !question.required || isAnswered(question, a, o)
    )

  function submitWith(a: Answers, o: Answers) {
    const content: Record<string, string> = {}
    for (const question of questions) {
      const other = o[question.id]?.trim() ?? ""
      if (other !== "") {
        // 纯自由输入题没有独立的 other 字段，答案直接写回题目本身。
        content[question.otherFieldId ?? question.id] = other
        continue
      }
      if (a[question.id]) content[question.id] = a[question.id]
    }
    setSubmitted(true)
    onResolve("accept", content)
  }

  /** 记录答案后前进：非末题翻页，末题在必答齐时立即提交。 */
  function advance(a: Answers, o: Answers) {
    if (!isLast) {
      setStep(step + 1)
      return
    }
    if (isComplete(a, o)) submitWith(a, o)
  }

  function pickOption(value: string) {
    const nextAnswers = { ...answers, [q.id]: value }
    const nextOthers = { ...others, [q.id]: "" }
    setAnswers(nextAnswers)
    setOthers(nextOthers)
    advance(nextAnswers, nextOthers)
  }

  const currentAnswered = isAnswered(q, answers, others)
  const canFinish = isComplete(answers, others)

  return (
    <div className="rounded-xl border border-border bg-card p-4 shadow-sm">
      <div className="mb-3 flex items-center gap-2 text-sm font-medium">
        <CircleHelpIcon className="size-4 text-primary" />
        {t("chat.elicitation.title")}
        {total > 1 ? (
          <span className="ml-auto text-xs font-normal text-muted-foreground">
            {step + 1} / {total}
          </span>
        ) : null}
      </div>

      {/* key 换题触发 remount，@starting-style 做轻快的换页淡入。 */}
      <div
        key={q.id}
        className="flex flex-col gap-2 transition-[opacity,translate] duration-200 ease-snappy starting:translate-x-1.5 starting:opacity-0 motion-reduce:starting:translate-x-0"
      >
        <div>
          <div className="text-sm font-medium">{q.title}</div>
          {q.description ? (
            <div className="text-sm text-muted-foreground">{q.description}</div>
          ) : null}
        </div>

        {q.options.length > 0 ? (
          <div className="flex flex-wrap gap-1.5">
            {q.options.map((option) => (
              <Button
                key={option.value}
                type="button"
                size="sm"
                variant={answers[q.id] === option.value ? "default" : "outline"}
                className="h-7 rounded-full text-xs"
                title={option.description}
                disabled={submitted}
                onClick={() => pickOption(option.value)}
              >
                {option.value}
              </Button>
            ))}
          </div>
        ) : null}

        {q.otherFieldId || q.options.length === 0 ? (
          <Input
            value={others[q.id] ?? ""}
            placeholder={t("chat.elicitation.otherPlaceholder")}
            className="h-8 text-sm"
            disabled={submitted}
            onChange={(e) => {
              const value = e.target.value
              setOthers((prev) => ({ ...prev, [q.id]: value }))
              if (value.trim() !== "") {
                setAnswers((prev) => ({ ...prev, [q.id]: "" }))
              }
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" && (others[q.id]?.trim() ?? "") !== "") {
                e.preventDefault()
                advance(answers, others)
              }
            }}
          />
        ) : null}
      </div>

      <div className="mt-4 flex items-center gap-2">
        {step > 0 ? (
          <Button
            size="sm"
            variant="ghost"
            disabled={submitted}
            onClick={() => setStep(step - 1)}
          >
            {t("chat.elicitation.back")}
          </Button>
        ) : null}
        {!isLast ? (
          <Button
            size="sm"
            variant="outline"
            disabled={submitted || (q.required && !currentAnswered)}
            onClick={() => setStep(step + 1)}
          >
            {t("chat.elicitation.next")}
          </Button>
        ) : (
          <Button
            size="sm"
            disabled={!canFinish || submitted}
            onClick={() => submitWith(answers, others)}
          >
            {t("chat.elicitation.submit")}
          </Button>
        )}
        <Button
          size="sm"
          variant="ghost"
          className="ml-auto text-muted-foreground"
          disabled={submitted}
          onClick={() => {
            setSubmitted(true)
            onResolve("decline")
          }}
        >
          {t("chat.elicitation.skip")}
        </Button>
      </div>
    </div>
  )
}

/** 已完成的交互式提问：每题显示全部选项，标出用户的选择，可随时回看。 */
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
    <div className="rounded-xl border border-border bg-card p-4 shadow-sm">
      <div className="mb-3 flex items-center gap-2 text-sm font-medium">
        <CircleHelpIcon className="size-4 text-primary" />
        {t("chat.elicitation.title")}
        {!accepted ? (
          <Badge variant="secondary" className="ml-auto">
            {t("chat.elicitation.skipped")}
          </Badge>
        ) : null}
      </div>

      {questions.length === 0 ? (
        <div className="text-sm text-muted-foreground">{message.content}</div>
      ) : (
        <div className="flex flex-col gap-3">
          {questions.map((q) => {
            const answer = answerFor(q, payload?.answers)
            const isCustom =
              answer !== "" && !q.options.some((o) => o.value === answer)
            return (
              <div key={q.id} className="flex flex-col gap-1.5">
                <div>
                  <div className="text-sm font-medium">{q.title}</div>
                  {q.description ? (
                    <div className="text-sm text-muted-foreground">
                      {q.description}
                    </div>
                  ) : null}
                </div>
                <div className="flex flex-wrap gap-1.5">
                  {q.options.map((option) => (
                    <span
                      key={option.value}
                      title={option.description}
                      className={cn(
                        "inline-flex h-7 items-center rounded-full border px-2.5 text-xs",
                        option.value === answer
                          ? "border-primary bg-primary text-primary-foreground"
                          : "border-border text-muted-foreground/70"
                      )}
                    >
                      {option.value}
                    </span>
                  ))}
                  {isCustom ? (
                    <span className="inline-flex h-7 items-center rounded-full border border-primary bg-primary px-2.5 text-xs text-primary-foreground">
                      {answer}
                    </span>
                  ) : null}
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
