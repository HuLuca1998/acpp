import { memo } from "react"
import { useTranslation } from "react-i18next"

import { AttachmentTray } from "@/components/chat/composer/attachment-tray"
import { ChatStream } from "@/components/chat/chat-stream"
import { Composer } from "@/components/chat/composer/composer"
import { ComposerStatus } from "@/components/chat/composer/composer-status"
import { QueuedMessages } from "@/components/chat/composer/queued-messages"
import { SettingsSelectors } from "@/components/chat/composer/settings-selectors"
import { useOrchCtx } from "@/components/orchestrator/orch-context"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { cn } from "@/lib/utils"
import { AtSignIcon, ImageIcon, NetworkIcon, OctagonXIcon } from "lucide-react"

/**
 * 编排主对话面板：消息流 + composer + 急停。spawn 的过程在主流里就是
 * 一个挂起的工具调用卡片，细节去任务面板看。
 */
export const OrchMainPanel = memo(function OrchMainPanel() {
  const { t } = useTranslation()
  const {
    isNew,
    chat,
    draft,
    setDraft,
    submit,
    draftCwd,
    openCwdPicker,
    images,
    files,
    removeImage,
    removeFile,
    addImages,
    openImagePicker,
    openFilePicker,
    recallQueued,
    steerQueued,
  } = useOrchCtx()

  const hasContent =
    chat.messages.length > 0 ||
    chat.streamingText !== "" ||
    chat.streamingThought !== "" ||
    chat.liveTools.length > 0 ||
    (chat.plan?.length ?? 0) > 0

  const runningTasks = chat.tasks.filter((t) => t.state === "running").length

  return (
    <div className="relative flex h-full min-h-0 flex-col bg-background">
      <div className="mx-auto w-full max-w-3xl px-4 pt-3 lg:px-6">
        {!isNew ? (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <span
              aria-label={
                chat.connected ? t("chat.connected") : t("chat.disconnected")
              }
              className={cn(
                "size-2 shrink-0 rounded-full",
                chat.connected ? "bg-success" : "bg-destructive",
                chat.connected &&
                  chat.busy &&
                  "animate-breathe motion-reduce:animate-none"
              )}
            />
            <span className="shrink-0 font-medium text-foreground">
              {chat.orchSession?.title || t("orch.untitled")}
            </span>
            {chat.orchSession?.tokensUsed ? (
              <span className="shrink-0 tabular-nums">
                {t("orch.tokensUsed", {
                  count: chat.orchSession.tokensUsed.toLocaleString() as never,
                })}
              </span>
            ) : null}
            {/* 急停：有在跑的任务或主轮时才可用。 */}
            {chat.busy || runningTasks > 0 ? (
              <Button
                size="sm"
                variant="destructive"
                className="ml-auto h-6"
                onClick={() => void chat.stopAll()}
              >
                <OctagonXIcon data-icon="inline-start" />
                {t("orch.stopAll")}
              </Button>
            ) : null}
          </div>
        ) : null}

        {chat.error ? (
          <Alert variant="destructive" className="mt-2">
            <AlertTitle>{t("errors.openFailed")}</AlertTitle>
            <AlertDescription>{chat.error}</AlertDescription>
          </Alert>
        ) : null}
      </div>

      <div className="min-h-0 flex-1 overflow-hidden">
        {!hasContent ? (
          <Empty className="h-full justify-center">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <NetworkIcon />
              </EmptyMedia>
              <EmptyTitle>{t("orch.emptyTitle")}</EmptyTitle>
              <EmptyDescription>{t("orch.emptyHint")}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <ChatStream chat={chat} />
        )}
      </div>

      <Composer
        value={draft}
        onChange={setDraft}
        onSubmit={submit}
        onCancel={isNew ? undefined : () => void chat.cancel()}
        busy={chat.busy}
        disabled={false}
        placeholder={t("orch.placeholder")}
        commands={chat.commands}
        attachments={
          <AttachmentTray
            images={images}
            files={files}
            onRemoveImage={removeImage}
            onRemoveFile={removeFile}
          />
        }
        onPasteImages={(picked) => addImages(picked)}
        queue={
          <QueuedMessages
            items={chat.queued}
            onSteer={steerQueued}
            onRecall={recallQueued}
          />
        }
        footer={
          <ComposerStatus
            cwd={isNew ? draftCwd : chat.orchSession?.cwd}
            usage={isNew ? null : chat.contextUsage}
            onPickCwd={isNew ? openCwdPicker : undefined}
          />
        }
      >
        {!isNew ? (
          <SettingsSelectors
            settings={chat.settings}
            disabled={chat.busy}
            onApply={chat.applySettings}
          />
        ) : null}
        <AttachmentButton
          label={t("chat.attachments.image")}
          onClick={openImagePicker}
        >
          <ImageIcon className="size-3.5" />
        </AttachmentButton>
        <AttachmentButton
          label={t("chat.attachments.file")}
          onClick={openFilePicker}
        >
          <AtSignIcon className="size-3.5" />
        </AttachmentButton>
      </Composer>
    </div>
  )
})

/** composer 里的附件圆钮（与普通会话面板同款）。 */
function AttachmentButton({
  label,
  onClick,
  children,
}: {
  label: string
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      aria-label={label}
      className="flex size-7 items-center justify-center rounded-full text-muted-foreground transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"
      onClick={onClick}
    >
      {children}
    </button>
  )
}
