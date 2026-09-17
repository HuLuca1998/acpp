// 用量报表的端点（token 与费用的轮次账本）。
//
// 从 index.ts 拆出来是因为那个文件到了行数硬线，而这一族本来就自成一块：
// 五个端点共用同一套筛选参数，且范围（owner 全量 / 租户自己）由后端按身份
// 收，前端不参与。展开进 `api` 对象，调用方仍然写 `api.usage.summary()`。

import type { Paged } from "@/types/acp"
import type {
  PriceTable,
  UsageBackfillResult,
  UsageBucket,
  UsageDimension,
  UsageErrors,
  UsageGroupRow,
  UsageQuery,
  UsageSummary,
} from "@/types/usage"

import { pageQuery, request } from "./core"

export const usageApi = {
  usage: {
    /**
     * 用量报表。筛选参数就是页面上那一排选择器，原样透传给后端——
     * 范围（owner 全量 / 租户自己）由后端按身份收，前端不需要也不该参与。
     */
    summary: (q?: UsageQuery) =>
      request<UsageSummary>(`/usage/summary${pageQuery({ ...q })}`),
    series: (q?: UsageQuery & { bucket?: "day" | "hour" }) =>
      request<Paged<UsageBucket>>(`/usage/series${pageQuery({ ...q })}`),
    breakdown: (q?: UsageQuery & { by?: UsageDimension; limit?: number }) =>
      request<Paged<UsageGroupRow>>(`/usage/breakdown${pageQuery({ ...q })}`),
    errors: (q?: UsageQuery & { limit?: number }) =>
      request<UsageErrors>(`/usage/errors${pageQuery({ ...q })}`),
    /** 折算单价表。读所有人可以，改是 owner 专属。 */
    prices: () => request<PriceTable>("/usage/prices"),
    /**
     * 保存单价表。**已经记下的账不会跟着变**——那是记账当时的价，改完
     * 要对齐得走 backfill。rev 由后端自增。
     */
    savePrices: (table: Omit<PriceTable, "rev">) =>
      request<PriceTable>("/usage/prices", {
        method: "PUT",
        body: JSON.stringify(table),
      }),
    /** 照转录重算全部历史账目（owner 专属）。幂等，可反复跑。 */
    backfill: () =>
      request<UsageBackfillResult>("/usage/backfill", { method: "POST" }),
  },
}
