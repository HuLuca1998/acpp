import { useTranslation } from "react-i18next"

import { api } from "@/lib/api"
import type { DiscordInfo } from "@/types/discord"
import { DiscordJobsCard } from "@/components/discord/jobs-card"
import { ListPageStates } from "@/components/list-page-states"
import { Card, CardContent } from "@/components/ui/card"
import { useAsyncData } from "@/hooks/use-async-data"
import { CalendarClockIcon } from "lucide-react"

/**
 * 定时任务页：侧栏的独立入口。任务挂在 Discord 频道绑定上（adr-020），
 * 数据与 Discord 页底部那块是同一份——这里只是让「管任务」不必先找到
 * Discord 页再往下翻。频道名与时区缺省仍从 /api/discord 的绑定清单取。
 */
export function Jobs() {
  const { t } = useTranslation()
  const { data: info, error } = useAsyncData<DiscordInfo>(
    () => api.discord.get(),
    []
  )

  return (
    <div className="flex flex-col gap-4 p-4 lg:p-6">
      {info ? (
        <DiscordJobsCard info={info} />
      ) : (
        <Card>
          <CardContent>
            <ListPageStates
              icon={<CalendarClockIcon className="size-6" />}
              error={error}
              loading={!error}
              emptyTitle={t("discord.jobs.empty")}
              emptyHint={t("discord.jobs.emptyHint")}
            />
          </CardContent>
        </Card>
      )}
    </div>
  )
}
