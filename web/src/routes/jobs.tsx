import { useTranslation } from "react-i18next"

import { api } from "@/lib/api"
import type { DiscordInfo } from "@/types/discord"
import { DiscordJobsCard } from "@/components/discord/jobs-card"
import { ListPageHeader } from "@/components/list-page-header"
import { ListPageStates } from "@/components/list-page-states"
import { Card, CardContent } from "@/components/ui/card"
import { useAsyncData } from "@/hooks/use-async-data"
import { CalendarClockIcon } from "lucide-react"

/**
 * 定时任务页：任务挂在 Discord 频道绑定上（adr-020），但只在这里管——
 * Discord 页只管频道绑定，不再把任务列表叠在下面。频道名与时区缺省
 * 从 /api/discord 的绑定清单取，所以要先拿到 info 才能画列表。
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
        // 绑定清单还没回来：页头照画，表格区那张卡先放骨架 / 错误，
        // 位置与 DiscordJobsCard 画出来的一致，加载完不跳。
        <>
          <ListPageHeader
            title={t("discord.jobs.title")}
            description={t("discord.jobs.description")}
          />
          <Card className="gap-0 py-0">
            <CardContent className="p-4">
              <ListPageStates
                icon={<CalendarClockIcon className="size-6" />}
                error={error}
                loading={!error}
                emptyTitle={t("discord.jobs.empty")}
                emptyHint={t("discord.jobs.emptyHint")}
              />
            </CardContent>
          </Card>
        </>
      )}
    </div>
  )
}
