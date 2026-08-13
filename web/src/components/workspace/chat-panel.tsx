import { memo, useContext } from "react"
import { useTranslation } from "react-i18next"

import { AttachmentTray } from "@/components/chat/attachment-tray"
import { ChatEmptyState } from "@/components/chat/chat-empty-state"
import { ChatStream } from "@/components/chat/chat-stream"
import { Composer } from "@/components/chat/composer"
import { ComposerStatus } from "@/components/chat/composer-status"
import { DraftControls } from "@/components/chat/draft-controls"
import { QueuedMessages } from "@/components/chat/queued-messages"
import { SettingsSelectors } from "@/components/chat/settings-selectors"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import {
  ChatPanelContext,
  type ChatPanelData,
} from "@/components/workspace/chat-panel-context"
import { cn } from "@/lib/utils"
import { AtSignIcon, ImageIcon } from "lucide-react"

function useChatPanel(): ChatPanelData {
  const value = useContext(ChatPanelContext)
  if (!value) throw new Error("ChatPanel must be used within ChatPanelContext")
  return value
}

/** composer 里的附件圆钮（图片上传 / @ 文件引用共用）。 */
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

/** 对话面板：消息流 + composer。工作区里唯一不可关闭的面板。 */
export const ChatPanel = memo(function ChatPanel() {
  const { t } = useTranslation()
  const {
    isNew,
    chat,
    newSession,
    draft,
    setDraft,
    images,
    files,
    removeImage,
    removeFile,
    submit,
    sendSuggestion,
    recallQueued,
    steerQueued,
    openImagePicker,
    openFilePicker,
    openCwdPicker,
    addImages,
    draftCwd,
  } = useChatPanel()

  const hasContent =
    chat.messages.length > 0 ||
    chat.streamingText !== "" ||
    chat.streamingThought !== "" ||
    chat.liveTools.length > 0 ||
    chat.permissions.length > 0 ||
    (chat.plan?.length ?? 0) > 0

  return (
    <div className="relative flex h-full min-h-0 flex-col bg-background">
      {/* 顶部信息条：连接状态 + 会话信息，保持轻量；草稿态没有会话可显示。 */}
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
            {chat.session ? (
              <>
                <span className="shrink-0 font-medium text-foreground">
                  {chat.session.title}
                </span>
                <span className="shrink-0">{chat.session.agentName}</span>
              </>
            ) : null}
          </div>
        ) : null}

        {chat.error || newSession.error ? (
          <Alert variant="destructive" className="mt-2">
            <AlertTitle>{t("errors.openFailed")}</AlertTitle>
            <AlertDescription>
              {chat.error ?? newSession.error}
            </AlertDescription>
          </Alert>
        ) : null}
      </div>

      {/* 消息流：底部 padding 给悬浮输入让位。 */}
      <div className="min-h-0 flex-1 overflow-hidden">
        {!hasContent ? (
          <ChatEmptyState
            disabled={
              isNew ? newSession.creating || !newSession.selected : chat.busy
            }
            onSuggestion={sendSuggestion}
          />
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
        pending={isNew && newSession.creating}
        disabled={isNew && (newSession.agents === null || !newSession.selected)}
        placeholder={t("chat.placeholder")}
        commands={
          isNew
            ? (newSession.selectedAgent?.commands ?? []).filter(
                (c) => !c.disabled
              )
            : chat.commands
        }
        attachments={
          <AttachmentTray
            images={images}
            files={files}
            onRemoveImage={removeImage}
            onRemoveFile={removeFile}
          />
        }
        onPasteImages={(picked) => void addImages(picked)}
        queue={
          <QueuedMessages
            items={chat.queued}
            onSteer={steerQueued}
            onRecall={recallQueued}
          />
        }
        footer={
          <ComposerStatus
            cwd={isNew ? draftCwd : chat.session?.cwd}
            gitBranch={isNew ? undefined : chat.session?.gitBranch}
            usage={isNew ? null : chat.contextUsage}
            onPickCwd={isNew ? openCwdPicker : undefined}
          />
        }
      >
        {isNew ? (
          <>
            <DraftControls draft={newSession} />
            {/* 模型之外的维度与会话态共用同一组件，三态工具栏显示一致；
                选择只在本地暂存，创建会话时随模型一起应用。 */}
            <SettingsSelectors
              settings={newSession.draftSettings}
              disabled={newSession.creating}
              onApply={newSession.applyDraftPatch}
            />
          </>
        ) : (
          <SettingsSelectors
            settings={chat.settings}
            disabled={chat.busy}
            onApply={chat.applySettings}
          />
        )}

        {/* 附件：图片上传与 @ 文件引用，两种态都可用。 */}
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
