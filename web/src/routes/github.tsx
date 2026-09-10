import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import type { ColumnDef } from "@tanstack/react-table"

import { ListPageHeader } from "@/components/list-page-header"
import { ListPageStates } from "@/components/list-page-states"
import { usePagedData } from "@/hooks/use-paged-data"
import { useSearchDraft } from "@/hooks/use-search-draft"
import { useIsOwner } from "@/hooks/identity-context"
import { DataTable } from "@/components/data-table/data-table"
import {
  SearchBar,
  SearchSelect,
  SearchText,
} from "@/components/data-table/data-table-search"
import { DataTableHeader } from "@/components/data-table/data-table-header"
import type { dataTableFeatures } from "@/components/data-table/data-table-features"
import { RepoDialog } from "@/components/github/repo-dialog"
import { api } from "@/lib/api"
import { formatRelativeTime } from "@/lib/format"
import { githubDotClass } from "@/lib/github-color"
import { cn } from "@/lib/utils"
import type {
  GithubColor,
  GithubIssue,
  GithubIssueResult,
} from "@/types/github"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { CircleDotIcon, FolderGit2Icon, TriangleAlertIcon } from "lucide-react"

type IssueColumn = ColumnDef<typeof dataTableFeatures, GithubIssue, unknown>

/** 列表响应里除分页之外的部分：筛选器词汇表与缓存新鲜度。 */
type IssueMeta = Omit<
  GithubIssueResult,
  "items" | "total" | "page" | "pageSize"
>

/** 状态点 + 名字：看板列与优先级共用，颜色取 GitHub 的色名。 */
function FieldDot({ name, color }: { name: string; color: GithubColor | "" }) {
  const { t } = useTranslation()
  if (!name) {
    return <span className="text-muted-foreground/50">{t("common.none")}</span>
  }
  return (
    <span className="inline-flex items-center gap-1.5">
      <span
        aria-hidden
        className={cn("size-1.5 shrink-0 rounded-full", githubDotClass(color))}
      />
      {name}
    </span>
  )
}

/**
 * GitHub issue 页（adr-023）：关注仓库里分配给我的 issue，带看板列与优先级。
 * 与其他列表页同一副四区骨架。默认条件：分配给我、open、排除做完 / 取消
 * 的看板列、按优先级排。
 */
export function Github() {
  const { t, i18n } = useTranslation()
  const isOwner = useIsOwner()
  const search = useSearchDraft({
    q: "",
    repo: "",
    assignee: "me",
    state: "open",
    board: "active",
    priority: "",
    label: "",
  })
  const { values } = search
  const [meta, setMeta] = useState<IssueMeta | null>(null)
  // 刷新按钮要的是「去 GitHub 重拉」而不是「再读一遍缓存」：按下时标记
  // 一次，下一次请求带 refresh=1。
  const forceRefresh = useRef(false)
  const {
    items,
    total,
    error,
    fetching,
    page,
    pageSize,
    sorting,
    setPage,
    setPageSize,
    setSorting,
    reload,
  } = usePagedData(
    async (params) => {
      const refresh = forceRefresh.current
      forceRefresh.current = false
      const res = await api.github.issues({
        ...params,
        repos: values.repo,
        q: values.q,
        assignee: values.assignee || "all",
        state: values.state || "all",
        board: values.board || "all",
        priority: values.priority,
        label: values.label,
        refresh: refresh ? "1" : undefined,
      })
      setMeta(res)
      return res
    },
    {
      sort: [{ id: "priority", desc: true }],
      deps: [values],
      keyOf: (issue) => `${issue.repo}#${issue.number}`,
    }
  )
  const submitSearch = () => {
    search.commit()
    setPage(1)
  }
  const resetSearch = () => {
    search.reset()
    setPage(1)
  }
  const refresh = () => {
    forceRefresh.current = true
    reload()
  }
  const [picking, setPicking] = useState(false)
  const watchedRepos = meta?.watched ?? []

  const columns: IssueColumn[] = [
    {
      id: "repo",
      enableSorting: false,
      header: t("github.repo"),
      meta: {
        label: t("github.repo"),
        className: "text-muted-foreground",
        pin: "left",
      },
      cell: ({ row }) => row.original.repo,
    },
    {
      id: "number",
      enableSorting: false,
      header: t("github.number"),
      meta: {
        label: t("github.number"),
        className: "font-mono text-xs text-muted-foreground tabular-nums",
      },
      cell: ({ row }) => `#${row.original.number}`,
    },
    {
      id: "title",
      enableSorting: false,
      enableHiding: false,
      header: t("github.issueTitle"),
      meta: { className: "max-w-xl" },
      cell: ({ row }) => (
        <a
          href={row.original.url}
          target="_blank"
          rel="noreferrer"
          title={row.original.title}
          className="block truncate font-medium after:absolute after:inset-0"
        >
          {row.original.title}
        </a>
      ),
    },
    {
      id: "labels",
      enableSorting: false,
      header: t("github.labels"),
      meta: { label: t("github.labels") },
      cell: ({ row }) =>
        row.original.labels.length > 0 ? (
          <span className="flex gap-1">
            {row.original.labels.map((label) => (
              <Badge
                key={label.name}
                variant="outline"
                className="text-xs font-normal text-muted-foreground"
              >
                {label.name}
              </Badge>
            ))}
          </span>
        ) : null,
    },
    {
      id: "priority",
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("github.priority")} />
      ),
      meta: { label: t("github.priority") },
      cell: ({ row }) => (
        <FieldDot
          name={row.original.priority}
          color={row.original.priorityColor}
        />
      ),
    },
    {
      id: "status",
      enableSorting: false,
      header: t("github.status"),
      meta: { label: t("github.status") },
      cell: ({ row }) => (
        <FieldDot name={row.original.status} color={row.original.statusColor} />
      ),
    },
    {
      id: "updated",
      header: ({ column }) => (
        <DataTableHeader column={column} title={t("github.updated")} />
      ),
      meta: {
        label: t("github.updated"),
        className: "text-muted-foreground tabular-nums",
      },
      cell: ({ row }) =>
        formatRelativeTime(row.original.updatedAt, i18n.language),
    },
  ]

  // 空态三种：没关注仓库（去关注）、租户没配 GitHub 用户名（找 owner）、
  // 条件下确实没有。
  const noWatch = meta !== null && watchedRepos.length === 0
  const noLogin = meta !== null && !meta.login && values.assignee === "me"
  const emptyHint = noWatch
    ? t("github.emptyWatchHint")
    : noLogin
      ? isOwner
        ? t("github.emptyLoginOwnerHint")
        : t("github.emptyLoginHint")
      : t("github.emptyHint")

  return (
    <div className="flex flex-col gap-4 p-4 lg:p-6">
      <ListPageHeader
        title={t("github.title")}
        total={items ? total : undefined}
      />
      {meta?.errors?.length ? (
        <Alert>
          <TriangleAlertIcon />
          <AlertTitle>{t("github.fetchErrors")}</AlertTitle>
          <AlertDescription>
            <ul className="font-mono text-xs">
              {meta.errors.map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      ) : null}
      <DataTable
        columns={columns}
        data={error ? null : items}
        total={total}
        page={page}
        pageSize={pageSize}
        sorting={sorting}
        search={
          <SearchBar onSearch={submitSearch} onReset={resetSearch}>
            <SearchSelect
              label={t("github.repo")}
              value={search.draft.repo}
              onChange={(v) => search.set("repo", v)}
              options={watchedRepos.map((r) => ({ value: r, label: r }))}
              width="lg"
            />
            <SearchText
              value={search.draft.q}
              onChange={(v) => search.set("q", v)}
              placeholder={t("github.searchTitle")}
            />
            <SearchSelect
              label={t("github.assignee")}
              value={search.draft.assignee}
              onChange={(v) => search.set("assignee", v)}
              options={[{ value: "me", label: t("github.assigneeMe") }]}
              width="sm"
            />
            <SearchSelect
              label={t("github.state")}
              value={search.draft.state}
              onChange={(v) => search.set("state", v)}
              options={[
                { value: "open", label: "Open" },
                { value: "closed", label: "Closed" },
              ]}
              width="sm"
            />
            <SearchSelect
              label={t("github.board")}
              value={search.draft.board}
              onChange={(v) => search.set("board", v)}
              options={[
                { value: "active", label: t("github.boardActive") },
                ...(meta?.statuses ?? []).map((s) => ({
                  value: s.name,
                  label: s.name,
                })),
              ]}
              width="sm"
            />
            <SearchSelect
              label={t("github.priority")}
              value={search.draft.priority}
              onChange={(v) => search.set("priority", v)}
              options={(meta?.priorities ?? []).map((p) => ({
                value: p.name,
                label: p.name,
              }))}
              width="sm"
            />
            <SearchSelect
              label={t("github.label")}
              value={search.draft.label}
              onChange={(v) => search.set("label", v)}
              options={(meta?.labels ?? []).map((l) => ({
                value: l,
                label: l,
              }))}
              width="sm"
            />
          </SearchBar>
        }
        fetching={fetching}
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPicking(true)}
            >
              <FolderGit2Icon data-icon="inline-start" />
              {t("github.watchRepos")}
              {watchedRepos.length > 0 ? (
                <span className="text-muted-foreground tabular-nums">
                  {watchedRepos.length}
                </span>
              ) : null}
            </Button>
            {meta?.statuses?.length ? (
              <span className="ml-2 hidden items-center gap-3 text-xs text-muted-foreground md:inline-flex">
                {meta.statuses.map((s) => (
                  <FieldDot key={s.name} name={s.name} color={s.color} />
                ))}
              </span>
            ) : null}
            {meta?.fetchedAt ? (
              <span
                className="ml-auto text-xs text-muted-foreground tabular-nums"
                title={meta.fetchedAt}
              >
                {t("github.fetchedAt", {
                  time: formatRelativeTime(meta.fetchedAt, i18n.language),
                })}
              </span>
            ) : null}
          </>
        }
        onReload={refresh}
        onPage={setPage}
        onPageSize={setPageSize}
        onSorting={setSorting}
        empty={
          <ListPageStates
            icon={<CircleDotIcon />}
            error={error}
            loading={items === null}
            emptyTitle={t("github.empty")}
            emptyHint={emptyHint}
            emptyAction={
              noWatch ? (
                <Button size="sm" onClick={() => setPicking(true)}>
                  <FolderGit2Icon data-icon="inline-start" />
                  {t("github.watchRepos")}
                </Button>
              ) : undefined
            }
          />
        }
      />

      <RepoDialog
        open={picking}
        onOpenChange={setPicking}
        onSaved={() => {
          // 关注清单变了，仓库筛选可能指着已取消关注的仓库：清掉再重拉。
          search.set("repo", "")
          setPage(1)
          reload()
        }}
      />
    </div>
  )
}
