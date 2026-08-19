import { memo } from "react"
import { useTranslation } from "react-i18next"

import {
  AttachmentButton,
  ReferenceMenu,
} from "@/components/chat/composer/attachment-buttons"
import { AttachmentTray } from "@/components/chat/composer/attachment-tray"
import { ChatStream } from "@/components/chat/chat-stream"
import { Composer } from "@/components/chat/composer/composer"
import { BranchPicker } from "@/components/chat/composer/branch-picker"
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
import { StatusDot } from "@/components/status-dot"
import { cn } from "@/lib/utils"
import { ImageIcon, NetworkIcon, OctagonXIcon } from "lucide-react"

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
    dbRefs,
    removeImage,
    removeFile,
    removeDbRef,
    addImages,
    openImagePicker,
    openFilePicker,
    openDbRefPicker,
    openUpload,
    recallQueued,
    steerQueued,
    openTaskPanel,
  } = useOrchCtx()

  const hasContent =
    chat.messages.length > 0 ||
    chat.streamingText !== "" ||
    chat.streamingThought !== "" ||
    chat.liveTools.length > 0 ||
    (chat.plan?.length ?? 0) > 0

  const runningList = chat.tasks.filter((task) => task.state === "running")
  const runningTasks = runningList.length

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

      {/* 活动条：任何时刻回答「系统是否在干活」。spawn 挂起时工具卡收在
          折叠区里不显眼，这里常驻显示在跑的任务（点击直达任务面板）；
          主控自己在跑而无任务时显示思考中。 */}
      {runningList.length > 0 || chat.busy ? (
        <div className="mx-auto flex w-full max-w-3xl flex-col gap-1 px-4 pt-2 lg:px-6">
          {runningList.map((task) => (
            <button
              key={task.id}
              type="button"
              onClick={() => openTaskPanel(task.id)}
              className="flex items-center gap-2 rounded-md border border-border/60 bg-card px-3 py-1.5 text-left text-xs transition-colors duration-150 ease-snappy hover:bg-accent"
            >
              <StatusDot tone="success" pulse />
              {/* 主标签不可被 flex 压缩成竖排字；摘要吃掉剩余空间单行截断。 */}
              <span className="shrink-0 font-medium whitespace-nowrap">
                {t("orch.working", { role: task.roleName })}
              </span>
              <span className="min-w-0 flex-1 truncate text-muted-foreground">
                {task.task}
              </span>
              <span className="shrink-0 whitespace-nowrap text-muted-foreground">
                {t("orch.viewTask")}
              </span>
            </button>
          ))}
          {runningList.length === 0 && chat.busy ? (
            <div className="flex items-center gap-2 px-3 py-1.5 text-xs text-muted-foreground">
              <StatusDot tone="success" pulse />
              <span className="text-shimmer">{t("orch.conductorWorking")}</span>
            </div>
          ) : null}
        </div>
      ) : null}

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
            dbRefs={dbRefs}
            onRemoveImage={removeImage}
            onRemoveFile={removeFile}
            onRemoveDbRef={removeDbRef}
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
            branchSlot={isNew ? null : <BranchPicker />}
            usage={isNew ? null : chat.contextUsage}
            lastUsage={isNew ? null : chat.lastUsage}
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
        <ReferenceMenu
          onPickFile={openFilePicker}
          onPickDatabase={openDbRefPicker}
          onUpload={openUpload}
        />
      </Composer>
    </div>
  )
})
