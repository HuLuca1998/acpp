import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Spinner } from "@/components/ui/spinner"
import { useIdentity } from "@/hooks/identity-context"
import { KeyRoundIcon, ShieldOffIcon, UnplugIcon } from "lucide-react"

/**
 * 身份门卫（adr-007）：认证通过才放行到应用本体。
 *
 * 三种拦下来的情形各有各的话要说——链接失效、被 owner 关停、后端不可达
 * 是完全不同的处境，混成一句「无法访问」只会让人反复刷新。
 */
export function IdentityGate({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation()
  const { identity, loading, error, refresh } = useIdentity()

  if (loading) {
    return (
      <div className="flex h-svh items-center justify-center">
        <Spinner className="size-5 text-muted-foreground" />
      </div>
    )
  }

  if (identity?.authenticated) return children

  const state = error
    ? {
        icon: <UnplugIcon />,
        title: t("identity.offlineTitle"),
        description: error,
      }
    : identity?.revoked
      ? {
          icon: <ShieldOffIcon />,
          title: t("identity.revokedTitle"),
          description: identity.tenantName
            ? t("identity.revokedDescNamed", { name: identity.tenantName })
            : t("identity.revokedDesc"),
        }
      : {
          icon: <KeyRoundIcon />,
          title: t("identity.inviteTitle"),
          description: t("identity.inviteDesc"),
        }

  return (
    <div className="flex h-svh items-center justify-center p-6">
      <Empty className="max-w-md">
        <EmptyHeader>
          <EmptyMedia variant="icon">{state.icon}</EmptyMedia>
          <EmptyTitle>{state.title}</EmptyTitle>
          <EmptyDescription>{state.description}</EmptyDescription>
        </EmptyHeader>
        <Button variant="outline" onClick={() => void refresh()}>
          {t("common.retry")}
        </Button>
      </Empty>
    </div>
  )
}
