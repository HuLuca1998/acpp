import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { api } from "@/lib/api"
import type { ModelPrice, PriceTable } from "@/types/usage"
import { InfoIcon } from "lucide-react"

/** 单价的五个字段，按「贵到便宜」排，填的时候心里有个量级。 */
const FIELDS = [
  "output",
  "cacheWrite",
  "input",
  "cacheRead",
  "thought",
] as const

const EMPTY: ModelPrice = {
  input: 0,
  output: 0,
  cacheRead: 0,
  cacheWrite: 0,
  thought: 0,
}

/**
 * 单价表编辑。
 *
 * 不预填任何价：模型 id 一个月里就能改，各家价目也在动——猜一个数字填
 * 进去，报表会拿它一路算下去，而没人知道它是编的。空着就是「未计价」，
 * 那是诚实的显示，不是缺陷。
 *
 * 两段：按工具的兜底价（claude 报的模型名多半只是档位，一个个配没有
 * 意义），与按模型的精确价（列的是这段时间里真实出现过的模型）。
 */
export function UsagePricesDialog({
  open,
  table,
  models,
  onClose,
  onSaved,
}: {
  open: boolean
  table: PriceTable | null
  /** 这段时间里真实出现过的模型 id，省得人自己去翻。 */
  models: string[]
  onClose: () => void
  onSaved: (table: PriceTable) => void
}) {
  const { t } = useTranslation()

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("usage.prices.title")}</DialogTitle>
          <DialogDescription>{t("usage.prices.description")}</DialogDescription>
        </DialogHeader>
        {open && (
          <PricesForm
            key={table?.rev ?? 0}
            table={table}
            models={models}
            onClose={onClose}
            onSaved={onSaved}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}

function PricesForm({
  table,
  models,
  onClose,
  onSaved,
}: {
  table: PriceTable | null
  models: string[]
  onClose: () => void
  onSaved: (table: PriceTable) => void
}) {
  const { t } = useTranslation()
  const [flavors, setFlavors] = useState<Record<string, ModelPrice>>({
    claude: table?.flavors?.claude ?? EMPTY,
    codex: table?.flavors?.codex ?? EMPTY,
  })
  const [perModel, setPerModel] = useState<Record<string, ModelPrice>>(() => {
    const seed: Record<string, ModelPrice> = { ...(table?.models ?? {}) }
    for (const id of models) {
      if (id && !seed[id]) seed[id] = EMPTY
    }
    return seed
  })
  const [saving, setSaving] = useState(false)

  const save = () => {
    setSaving(true)
    api.usage
      .savePrices({
        // 全 0 的行不必留在表里：后端把它当「没配」，存着只会让配置文件
        // 越长越难读。
        flavors: prune(flavors),
        models: prune(perModel),
      })
      .then((saved) => {
        toast.success(t("usage.prices.saved"))
        onSaved(saved)
        onClose()
      })
      .catch((err: Error) => toast.error(err.message))
      .finally(() => setSaving(false))
  }

  return (
    <div className="flex flex-col gap-5">
      <p className="flex items-start gap-2 rounded-lg bg-muted p-3 text-xs leading-relaxed text-muted-foreground">
        <InfoIcon className="mt-0.5 size-3.5 shrink-0" />
        {t("usage.prices.hint")}
      </p>

      <section className="flex flex-col gap-2">
        <h3 className="text-sm font-medium">{t("usage.prices.byFlavor")}</h3>
        <p className="text-xs text-muted-foreground">
          {t("usage.prices.byFlavorHint")}
        </p>
        <PriceGrid
          rows={["claude", "codex"]}
          values={flavors}
          onChange={setFlavors}
        />
      </section>

      {Object.keys(perModel).length > 0 && (
        <section className="flex flex-col gap-2">
          <h3 className="text-sm font-medium">{t("usage.prices.byModel")}</h3>
          <p className="text-xs text-muted-foreground">
            {t("usage.prices.byModelHint")}
          </p>
          <PriceGrid
            rows={Object.keys(perModel).sort()}
            values={perModel}
            onChange={setPerModel}
          />
        </section>
      )}

      <DialogFooter>
        <Button variant="outline" onClick={onClose} disabled={saving}>
          {t("common.cancel")}
        </Button>
        <Button onClick={save} disabled={saving}>
          {t("common.save")}
        </Button>
      </DialogFooter>
    </div>
  )
}

/** 一组价的表格：行是模型或方言，列是五个字段。 */
function PriceGrid({
  rows,
  values,
  onChange,
}: {
  rows: string[]
  values: Record<string, ModelPrice>
  onChange: (next: Record<string, ModelPrice>) => void
}) {
  const { t } = useTranslation()

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-xs">
        <thead>
          <tr className="text-muted-foreground">
            <th className="w-40 pb-1.5 text-left font-medium">
              {t("usage.prices.colTarget")}
            </th>
            {FIELDS.map((f) => (
              <th key={f} className="pb-1.5 pl-2 text-left font-medium">
                {t(`usage.prices.field.${f}`)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((key) => (
            <tr key={key}>
              <td className="py-1 pr-2 font-mono">{key}</td>
              {FIELDS.map((field) => (
                <td key={field} className="py-1 pl-2">
                  <Input
                    type="number"
                    min={0}
                    step="0.01"
                    inputMode="decimal"
                    className="h-8 tabular-nums"
                    aria-label={`${key} ${t(`usage.prices.field.${field}`)}`}
                    value={values[key]?.[field] || ""}
                    placeholder="—"
                    onChange={(e) =>
                      onChange({
                        ...values,
                        [key]: {
                          ...(values[key] ?? EMPTY),
                          [field]: Number(e.target.value) || 0,
                        },
                      })
                    }
                  />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

/** 去掉全 0 的行：后端把它当「没配」，存着只会让配置越长越难读。 */
function prune(
  values: Record<string, ModelPrice>
): Record<string, ModelPrice> | undefined {
  const out: Record<string, ModelPrice> = {}
  for (const [key, price] of Object.entries(values)) {
    if (FIELDS.some((f) => (price[f] ?? 0) > 0)) out[key] = price
  }
  return Object.keys(out).length > 0 ? out : undefined
}
