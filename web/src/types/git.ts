// git 域的领域类型（工作区 git 面板群 + 分支控件），与 server/internal/service
// 的对应 DTO 对齐。从 ./acp 原样转出（同 ./db 的拆法）：`@/types/acp` 仍是
// 领域类型的单一入口，调用方不必记住哪个类型住在哪个文件里。

/** 一条文件级 git 变更，与 service.GitFileChange 对齐；-1 表示无行数概念。 */
export interface GitFileChange {
  path: string
  status: string
  added: number
  deleted: number
}

export interface GitCommit {
  sha: string
  short: string
  subject: string
  author: string
  time: number
}

/** diff / commit 面板共享的一次性视图，与 service.GitOverview 对齐。 */
export interface GitOverview {
  isRepo: boolean
  /** 仓库根绝对路径：files 里的路径相对它，界面靠它对应到文件树条目。 */
  root?: string
  branch?: string
  upstream?: string
  ahead: number
  behind: number
  files: GitFileChange[]
  commits: GitCommit[]
}

/** 单文件 diff 的两端全文，行级对齐在前端做。 */
export interface GitDiffView {
  path: string
  oldText: string
  newText: string
  binary?: boolean
  truncated?: boolean
}

export interface GitCommitDetail {
  commit: GitCommit
  files: GitFileChange[]
}

/** 分支清单里的一条。worktree 非空表示已被别的工作区占用，不能切。 */
export interface GitBranch {
  name: string
  current: boolean
  worktree?: string
}

/** 一个 git 工作区（含主工作区）。 */
export interface GitWorktree {
  path: string
  branch?: string
  main: boolean
  current: boolean
}

/** 会话底部分支控件的全部数据。 */
export interface GitBranchView {
  isRepo: boolean
  current?: string
  /** detached 时 current 是短 hash，不是分支名。 */
  detached: boolean
  /** 有未提交改动：脏工作区不允许切分支。 */
  dirty: boolean
  local: GitBranch[]
  remote: string[]
  tags: string[]
  worktrees: GitWorktree[]
}

/** 一次 git 写操作的结果：git 的原话 + 操作后的分支视图。 */
export interface GitOpResult {
  output?: string
  branch?: GitBranchView
}

/** 提交链路的一页。 */
export interface GitHistory {
  commits: GitCommit[]
  hasMore: boolean
}

/** 两个 ref 的对比：head 相对 base 多出的提交与文件变更（三点 diff）。 */
export interface GitCompare {
  base: string
  head: string
  ahead: number
  behind: number
  commits: GitCommit[]
  files: GitFileChange[]
}
