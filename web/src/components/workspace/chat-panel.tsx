import { memo, useContext } from "react"
import { useTranslation } from "react-i18next"

import {
  ChatPanelContext,
  type ChatPanelData,
} from "@/components/workspace/chat-panel-context"
import { cn } from "@/lib/utils"
import { AttachmentTray } from "@/components/chat/attachment-tray"
import {
  ActivityMessage,
  ActivitySection,
  ChatMessage,
  EarlierSentinel,
  LiveToolMarker,
} from "@/components/chat/chat-messages"
import { groupMessages } from "@/lib/message-blocks"
import { Composer } from "@/components/chat/composer"
import { ComposerStatus } from "@/components/chat/composer-status"
import { DraftControls } from "@/components/chat/draft-controls"
import { ElicitationCard } from "@/components/chat/elicitation-card"
import { MarkdownContent } from "@/components/chat/markdown"
import { PermissionCard } from "@/components/chat/permission-card"
import { PlanCard } from "@/components/chat/plan-card"
import { PlanReviewCard } from "@/components/chat/plan-review-card"
import { SettingsSelectors } from "@/components/chat/settings-selectors"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Marker, MarkerContent, MarkerIcon } from "@/components/ui/marker"
import {
  MessageScroller,
  MessageScrollerButton,
  MessageScrollerContent,
  MessageScrollerItem,
  MessageScrollerProvider,
  MessageScrollerViewport,
} from "@/components/ui/message-scroller"
import { Spinner } from "@/components/ui/spinner"
import {
  AtSignIcon,
  BrainIcon,
  CircleAlertIcon,
  ImageIcon,
  MessagesSquareIcon,
  ShieldCheckIcon,
} from "lucide-react"

function useChatPanel(): ChatPanelData {
  const value = useContext(ChatPanelContext)
  if (!value) throw new Error("ChatPanel must be used within ChatPanelContext")
  return value
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
    openImagePicker,
    openFilePicker,
    openCwdPicker,
    addImages,
    draftCwd,
  } = useChatPanel()

  const liveActivityCount =
    (chat.streamingThought ? 1 : 0) +
    chat.liveTools.length +
    chat.permissions.length
  const hasContent =
    chat.messages.length > 0 ||
    chat.streamingText !== "" ||
    liveActivityCount > 0 ||
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
          <Empty className="h-full justify-center">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <MessagesSquareIcon />
              </EmptyMedia>
              <EmptyTitle>{t("chat.empty")}</EmptyTitle>
              <EmptyDescription>{t("chat.emptyHint")}</EmptyDescription>
            </EmptyHeader>
            {/* 建议芯片：给第一句一个起点，点击即发送。 */}
            <EmptyContent>
              <div className="flex flex-wrap justify-center gap-2">
                {(["intro", "changes", "help"] as const).map((key) => (
                  <Button
                    key={key}
                    variant="outline"
                    size="sm"
                    className="rounded-full"
                    disabled={
                      isNew
                        ? newSession.creating || !newSession.selected
                        : chat.busy
                    }
                    onClick={() => sendSuggestion(t(`chat.suggestions.${key}`))}
                  >
                    {t(`chat.suggestions.${key}`)}
                  </Button>
                ))}
              </div>
            </EmptyContent>
          </Empty>
        ) : (
          // 打开即定位到底部看最新内容（不是先渲染顶部再跳）；
          // 「加载更早」prepend 时保持视口位置不跳。
          <MessageScrollerProvider defaultScrollPosition="end">
            <MessageScroller>
              <MessageScrollerViewport preserveScrollOnPrepend>
                <MessageScrollerContent className="mx-auto w-full max-w-3xl px-4 pt-4 pb-48 lg:px-6">
                  {chat.hasEarlier ? (
                    <MessageScrollerItem scrollAnchor={false}>
                      <EarlierSentinel
                        onVisible={() => void chat.loadEarlier()}
                      />
                    </MessageScrollerItem>
                  ) : null}
                  {groupMessages(chat.messages).map((block) =>
                    block.type === "chat" ? (
                      <MessageScrollerItem
                        key={block.message.id}
                        messageId={String(block.message.id)}
                        scrollAnchor={block.message.role === "user"}
                      >
                        <ChatMessage message={block.message} />
                      </MessageScrollerItem>
                    ) : (
                      <MessageScrollerItem key={block.key} scrollAnchor={false}>
                        <ActivitySection count={block.items.length}>
                          {block.items.map((item) => (
                            <ActivityMessage key={item.id} message={item} />
                          ))}
                        </ActivitySection>
                      </MessageScrollerItem>
                    )
                  )}

                  {/* 任务计划：随 plan 事件实时更新，轮次结束保留最终状态。 */}
                  {chat.plan && chat.plan.length > 0 ? (
                    <MessageScrollerItem scrollAnchor={false}>
                      <PlanCard entries={chat.plan} />
                    </MessageScrollerItem>
                  ) : null}

                  {liveActivityCount > 0 ? (
                    <MessageScrollerItem scrollAnchor={false}>
                      <ActivitySection
                        count={liveActivityCount}
                        busy={chat.busy}
                      >
                        {chat.streamingThought ? (
                          <Marker>
                            <MarkerIcon>
                              <BrainIcon />
                            </MarkerIcon>
                            <MarkerContent>
                              <span className="text-shimmer">
                                {t("chat.thinking")}
                              </span>
                              {/* 只看思考的"最新进展"：截尾 + 行数钳制，不淹没界面。 */}
                              <div className="mt-1 line-clamp-3 whitespace-pre-wrap">
                                {chat.streamingThought.slice(-600)}
                              </div>
                            </MarkerContent>
                          </Marker>
                        ) : null}
                        {chat.permissions.map((perm) => (
                          <Marker key={perm.id}>
                            <MarkerIcon>
                              <ShieldCheckIcon />
                            </MarkerIcon>
                            <MarkerContent>
                              {t("chat.permission.resolved", {
                                title: perm.title,
                                choice:
                                  perm.choice || t("chat.permission.cancelled"),
                              })}
                            </MarkerContent>
                          </Marker>
                        ))}
                        {chat.liveTools.map((tool) => (
                          <LiveToolMarker key={tool.id} tool={tool} />
                        ))}
                      </ActivitySection>
                    </MessageScrollerItem>
                  ) : null}

                  {chat.streamingText ? (
                    <MessageScrollerItem scrollAnchor={false}>
                      <MarkdownContent>{chat.streamingText}</MarkdownContent>
                    </MessageScrollerItem>
                  ) : null}

                  {chat.permission ? (
                    <MessageScrollerItem
                      key={chat.permission.id}
                      scrollAnchor={false}
                    >
                      {chat.permission.planReview ? (
                        <PlanReviewCard
                          permission={chat.permission}
                          onResolve={(optionId, choiceName) =>
                            void chat.resolvePermission(
                              chat.permission!.id,
                              optionId,
                              choiceName
                            )
                          }
                        />
                      ) : (
                        <PermissionCard
                          permission={chat.permission}
                          onResolve={(optionId, choiceName) =>
                            void chat.resolvePermission(
                              chat.permission!.id,
                              optionId,
                              choiceName
                            )
                          }
                        />
                      )}
                    </MessageScrollerItem>
                  ) : null}

                  {chat.elicitation ? (
                    <MessageScrollerItem
                      key={chat.elicitation.id}
                      scrollAnchor={false}
                    >
                      <ElicitationCard
                        elicitation={chat.elicitation}
                        onResolve={(action, content) =>
                          void chat.resolveElicitation(
                            chat.elicitation!.id,
                            action,
                            content
                          )
                        }
                      />
                    </MessageScrollerItem>
                  ) : null}

                  {chat.busy &&
                  !chat.streamingText &&
                  liveActivityCount === 0 &&
                  !chat.elicitation &&
                  !chat.permission ? (
                    <MessageScrollerItem scrollAnchor={false}>
                      <Marker role="status">
                        <MarkerIcon>
                          <Spinner />
                        </MarkerIcon>
                        <MarkerContent className="text-shimmer">
                          {t("chat.thinking")}
                        </MarkerContent>
                      </Marker>
                    </MessageScrollerItem>
                  ) : null}

                  {/* 非正常结束原因内联在消息流末尾，紧跟被截断的回答。 */}
                  {chat.stopReason ? (
                    <MessageScrollerItem scrollAnchor={false}>
                      <Marker>
                        <MarkerIcon>
                          <CircleAlertIcon className="text-warning" />
                        </MarkerIcon>
                        <MarkerContent>
                          {t(`chat.stopReason.${chat.stopReason}` as never, {
                            defaultValue: chat.stopReason,
                          })}
                        </MarkerContent>
                      </Marker>
                    </MessageScrollerItem>
                  ) : null}
                </MessageScrollerContent>
              </MessageScrollerViewport>
              <MessageScrollerButton className="data-[direction=end]:bottom-40" />
            </MessageScroller>
          </MessageScrollerProvider>
        )}
      </div>

      <Composer
        value={draft}
        onChange={setDraft}
        onSubmit={submit}
        onCancel={isNew ? undefined : () => void chat.cancel()}
        busy={chat.busy}
        pending={newSession.creating}
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
          <DraftControls draft={newSession} />
        ) : (
          <SettingsSelectors
            settings={chat.settings}
            disabled={chat.busy}
            onApply={chat.applySettings}
          />
        )}

        {/* 附件：图片上传与 @ 文件引用，两种态都可用。 */}
        <button
          type="button"
          aria-label={t("chat.attachments.image")}
          className="flex size-7 items-center justify-center rounded-full text-muted-foreground transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"
          onClick={openImagePicker}
        >
          <ImageIcon className="size-3.5" />
        </button>
        <button
          type="button"
          aria-label={t("chat.attachments.file")}
          className="flex size-7 items-center justify-center rounded-full text-muted-foreground transition-[scale,background-color,color] duration-150 ease-snappy hover:bg-muted hover:text-foreground active:scale-[0.97]"
          onClick={openFilePicker}
        >
          <AtSignIcon className="size-3.5" />
        </button>
      </Composer>
    </div>
  )
})
