import { memo, useState } from "react"
import { useTranslation } from "react-i18next"

import {
  AttachmentButton,
  ReferenceMenu,
} from "@/components/chat/composer/attachment-buttons"
import { AttachmentTray } from "@/components/chat/composer/attachment-tray"
import { DbSlashPanel } from "@/components/db/db-slash-panel"
import { ChatEmptyState } from "@/components/chat/chat-empty-state"
import { ChatStream } from "@/components/chat/chat-stream"
import { Composer } from "@/components/chat/composer/composer"
import { BranchPicker } from "@/components/chat/composer/branch-picker"
import { ComposerStatus } from "@/components/chat/composer/composer-status"
import { WorktreeToggle } from "@/components/chat/composer/worktree-toggle"
import { DraftControls } from "@/components/chat/composer/draft-controls"
import { QueuedMessages } from "@/components/chat/composer/queued-messages"
import { SettingsSelectors } from "@/components/chat/composer/settings-selectors"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import {
  useChatPanel,
} from "@/components/workspace/chat-panel-context"
import { useWorkspace } from "@/components/workspace/workspace-context"
import { parseLocalCommand, withLocalCommands } from "@/lib/local-commands"
import { cn } from "@/lib/utils"
import { ImageIcon } from "lucide-react"

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
    dbRefs,
    removeImage,
    removeFile,
    removeDbRef,
    addDbRef,
    submit,
    sendSuggestion,
    recallQueued,
    steerQueued,
    openImagePicker,
    openFilePicker,
    openDbRefPicker,
    openUpload,
    openCwdPicker,
    addImages,
    draftCwd,
  } = useChatPanel()

  // 本地斜杠命令的结果（目前只有 /db）：浮在输入框上方，不进对话流。
  // null 表示没在看。
  const [localCommand, setLocalCommand] = useState<string | null>(null)
  const sessionId = chat.session?.id ?? 0
  // 数据面作用域：会话态查会话的项目，草稿态经 `/workspace?cwd=` 查
  // 选定目录的——/db 因此不用等会话建出来。
  const workspace = useWorkspace()
  // 本地命令有项目可查就放行：会话已建，或草稿态已选工作目录。
  const localReady = sessionId > 0 || Boolean(isNew && draftCwd)

  // 本地命令自己消化掉，不发给 agent；其余照常提交。
  function handleSubmit() {
    const local = parseLocalCommand(draft)
    if (local && localReady) {
      setLocalCommand(local.args)
      setDraft("")
      return
    }
    submit()
  }

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
        onSubmit={handleSubmit}
        onCancel={isNew ? undefined : () => void chat.cancel()}
        busy={chat.busy}
        pending={isNew && newSession.creating}
        disabled={isNew && (newSession.agents === null || !newSession.selected)}
        placeholder={t("chat.placeholder")}
        commands={(() => {
          // 草稿态选了工作目录，/db 就有项目可查；没选目录不列——
          // 列一个按了没反应的命令比没有更糟。
          const agentCommands = isNew
            ? (newSession.selectedAgent?.commands ?? []).filter(
                (c) => !c.disabled
              )
            : chat.commands
          return localReady
            ? withLocalCommands(agentCommands, { db: t("db.slashHint") })
            : agentCommands
        })()}
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
        onPasteImages={(picked) => void addImages(picked)}
        localPanel={
          localCommand !== null && localReady ? (
            <DbSlashPanel
              sessionId={sessionId}
              scope={workspace.scope}
              args={localCommand}
              onPick={addDbRef}
              onClose={() => setLocalCommand(null)}
            />
          ) : null
        }
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
            branchSlot={
              isNew ? null : <BranchPicker fallback={chat.session?.gitBranch} />
            }
            worktreeSlot={
              isNew ? (
                <WorktreeToggle
                  value={newSession.worktree}
                  onChange={newSession.setWorktree}
                />
              ) : null
            }
            usage={isNew ? null : chat.contextUsage}
            lastUsage={isNew ? null : chat.lastUsage}
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
        <ReferenceMenu
          onPickFile={openFilePicker}
          onPickDatabase={openDbRefPicker}
          onUpload={openUpload}
        />
      </Composer>
    </div>
  )
})
