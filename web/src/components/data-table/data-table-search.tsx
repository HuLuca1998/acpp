import type { ReactNode } from "react"
import { useTranslation } from "react-i18next"

import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { SearchIcon } from "lucide-react"

/**
 * 列表页的搜索区：一排控件 + 「查询」「重置」。
 *
 * 草稿与生效值分开：输入框里改的是草稿，点「查询」（或回车）才提交去
 * 请求。边打字边查在服务端分页的列表上是错的——每个字符一次往返，请求
 * 回来的顺序还不一定和敲的顺序一样，表格会在几种结果之间跳。
 *
 * 顶部对齐而不是居中：控件高度一致时看不出区别，将来有带副行的控件
 * （区间下面挂快捷项）也不会把整行顶歪。
 */
export function SearchBar({
  onSearch,
  onReset,
  children,
}: {
  onSearch: () => void
  onReset: () => void
  children: ReactNode
}) {
  const { t } = useTranslation()
  return (
    <form
      className="flex flex-wrap items-start gap-2"
      onSubmit={(e) => {
        e.preventDefault()
        onSearch()
      }}
    >
      {children}
      <div className="flex items-center gap-1">
        <Button type="submit" size="sm">
          <SearchIcon data-icon="inline-start" />
          {t("table.search")}
        </Button>
        <Button type="button" size="sm" variant="ghost" onClick={onReset}>
          {t("table.reset")}
        </Button>
      </div>
    </form>
  )
}

/** 搜索控件的三档宽度，与 gosaic 的搜索区同一套。 */
const SEARCH_WIDTHS = {
  sm: "w-28",
  md: "w-40",
  lg: "w-56",
} as const

/** 关键词输入框。 */
export function SearchText({
  value,
  onChange,
  placeholder,
  width = "md",
}: {
  value: string
  onChange: (value: string) => void
  placeholder: string
  width?: keyof typeof SEARCH_WIDTHS
}) {
  return (
    <Input
      className={cn("h-8", SEARCH_WIDTHS[width])}
      value={value}
      placeholder={placeholder}
      onChange={(e) => onChange(e.target.value)}
      // 回车显式走 requestSubmit：浏览器的隐式提交要求按键事件是可信的，
      // 桌面壳与自动化里合成的按键不算，只靠隐式提交回车就会没反应。
      onKeyDown={(e) => {
        if (e.key !== "Enter") return
        e.preventDefault()
        e.currentTarget.form?.requestSubmit()
      }}
    />
  )
}

/** 「全部」选项的值：Select 不接受空串当值，用一个不会与真实取值撞车的哨兵。 */
const ALL = "__all__"

/**
 * 枚举下拉。关着的时候显示「标签：当前值」——一排下拉都只写着「全部」时
 * 分不清哪个是哪个。
 */
export function SearchSelect({
  label,
  value,
  onChange,
  options,
  width = "md",
}: {
  label: string
  /** 空串表示「全部」。 */
  value: string
  onChange: (value: string) => void
  options: { value: string; label: string }[]
  width?: keyof typeof SEARCH_WIDTHS
}) {
  const { t } = useTranslation()
  const items = [{ value: ALL, label: t("table.all") }, ...options]
  const current = items.find((o) => o.value === (value || ALL))
  return (
    <Select
      value={value || ALL}
      onValueChange={(v) => onChange(v === ALL || v === null ? "" : v)}
    >
      <SelectTrigger className={cn("h-8", SEARCH_WIDTHS[width])}>
        <SelectValue>
          <span className="text-muted-foreground">{label}：</span>
          {current?.label}
        </SelectValue>
      </SelectTrigger>
      <SelectContent>
        {items.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
