// GitHub issue 页的领域类型，与 server/internal/github 对齐（adr-023）。

/** issue 标签；color 是 GitHub 的六位 hex（不带 #）。 */
export interface GithubLabel {
  name: string
  color: string
}

/**
 * GitHub 的单选字段颜色名：看板列与 Priority 都用这套枚举。
 * 映射到状态点的类名见 lib/github-color.ts。
 */
export type GithubColor =
  "GRAY" | "BLUE" | "GREEN" | "YELLOW" | "ORANGE" | "RED" | "PINK" | "PURPLE"

/** 聚合后的单条 issue。priority / status 读不到时为空串。 */
export interface GithubIssue {
  repo: string
  number: number
  title: string
  url: string
  state: "OPEN" | "CLOSED"
  labels: GithubLabel[]
  assignees: string[]
  author: string
  updatedAt: string
  /** issue 侧栏 Fields 里的优先级（Urgent / High / …）。 */
  priority: string
  priorityColor: GithubColor | ""
  /** 所属 Project 看板的列名（待处理 / 进行中 / …）。 */
  status: string
  statusColor: GithubColor | ""
}

/** 单选字段的一个选项：看板列或优先级档，顺序即 GitHub 上声明的顺序。 */
export interface GithubOption {
  name: string
  color: GithubColor
}

/** 一次 issue 查询：一页 issue + 画筛选器要用的词汇表 + 缓存新鲜度。 */
export interface GithubIssueResult {
  items: GithubIssue[]
  total: number
  page: number
  pageSize: number
  statuses: GithubOption[]
  priorities: GithubOption[]
  labels: string[]
  /** 当前身份的关注清单；仓库筛选项直接取它。 */
  watched: string[]
  /** 「分配给我」实际用的 GitHub 用户名；空表示这个身份没配。 */
  login: string
  /** 最旧的那个仓库缓存的拉取时间；没拉到过就没有。 */
  fetchedAt?: string
  /** 拉取失败的仓库及原因；部分失败不拦住其余仓库。 */
  errors?: string[]
}

/** 可关注的仓库。 */
export interface GithubRepo {
  name: string
  private: boolean
  watched: boolean
}
