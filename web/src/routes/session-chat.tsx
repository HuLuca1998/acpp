import { useCallback, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useParams } from "react-router"

import { useChat } from "@/hooks/use-chat"
import { useDraftSession } from "@/hooks/use-draft-session"
import { useIdentity } from "@/hooks/identity-context"
import type { ImageAttachment } from "@/types/acp"
import { fileToImageAttachment } from "@/lib/files"
import { api } from "@/lib/api"
import { DbRefPicker } from "@/components/db/db-ref-picker"
import { UploadDialog } from "@/components/chat/composer/upload-dialog"
import { DirPicker } from "@/components/dir-picker/dir-picker"
import {
  ChatPanelContext,
  createDraftStore,
  type ChatPanelData,
} from "@/components/workspace/chat-panel-context"
import { WorkspaceDock } from "@/components/workspace/workspace-dock"
import {
  WorkspaceAutoRefresh,
  WorkspaceProvider,
  WorkspaceAskSink,
  WorkspaceReferenceSink,
} from "@/components/workspace/workspace-provider"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { MessagesSquareIcon } from "lucide-react"

/**
 * 会话工作区宿主页（adr-002）：页面层持有会话流与草稿状态，
 * 面板经 dockview 编排；对话面板通过 ChatPanelContext 消费这里的状态，
 * 被拖动重挂载也不丢流。
 */
export function SessionChat() {
  const { t } = useTranslation()
  const params = useParams()
  // /sessions/new 没有 :id —— 草稿态：会话未创建，首条消息落地才建。
  const isNew = params.id === undefined
  const sessionId = isNew ? 0 : Number(params.id)

  const chat = useChat(sessionId)
  const newSession = useDraftSession(isNew, t("sessions.form.defaultModel"))
  // 草稿住在 ref 化的 store 里而不是页面 state：打字不该重渲整棵工作区树
  //（见 chat-panel-context 的 DraftStore 注释）。
  const [draftStore] = useState(createDraftStore)

  // 待发送附件：图片（粘贴/选择）与 @ 引用的文件路径。
  const { identity } = useIdentity()
  const [images, setImages] = useState<ImageAttachment[]>([])
  const [files, setFiles] = useState<string[]>([])
  const [dbRefs, setDbRefs] = useState<string[]>([])

  // 加引用的唯一入口：@ 菜单与 /db 面板都走它，重复的不再加一遍。
  const addDbRef = useCallback((ref: string) => {
    setDbRefs((prev) => (prev.includes(ref) ? prev : [...prev, ref]))
  }, [])
  const [filePickerOpen, setFilePickerOpen] = useState(false)
  const [dbRefPickerOpen, setDbRefPickerOpen] = useState(false)
  const [uploadOpen, setUploadOpen] = useState(false)
  const [cwdPickerOpen, setCwdPickerOpen] = useState(false)
  const imageInputRef = useRef<HTMLInputElement>(null)

  // 草稿态**不预设**工作目录：没选就显示这个身份的起点作为提示，由后端
  // 落到那儿。以前会回落到 agent 记录里的 cwd——那是历史残留，不该替
  // 用户决定他这次在哪儿干活。访客看到的是自己的目录，不是通用的 ~/acpp。
  const draftCwd =
    newSession.cwd.trim() || identity?.root || t("sessions.form.cwdPlaceholder")

  async function addImages(picked: File[]) {
    const attachments = await Promise.all(picked.map(fileToImageAttachment))
    setImages((prev) => [...prev, ...attachments])
  }

  function submit() {
    const content = draftStore.get().trim()
    if (
      !content &&
      images.length === 0 &&
      files.length === 0 &&
      dbRefs.length === 0
    )
      return
    const input = { content, images, files, datasources: dbRefs }
    if (isNew) {
      if (!newSession.selected || newSession.creating) return
      // 组件跨 /sessions/new → /sessions/:id 复用不重挂，不清会残留到会话页。
      draftStore.set("")
      setImages([])
      setFiles([])
      setDbRefs([])
      void newSession.start(input)
      return
    }
    draftStore.set("")
    setImages([])
    setFiles([])
    setDbRefs([])
    // 一轮进行中：插话不直接发，先排队浮在输入框上方，轮次结束自动发出；
    // 排队条上可「调整方向」立即插入当前轮，或撤回回填输入框。
    if (chat.busy) {
      chat.enqueue(input)
    } else {
      void chat.send(input)
    }
  }

  /** 撤回一条排队插话：从队列移除并回填输入框与附件托盘。 */
  function recallQueued(id: number) {
    const item = chat.queued.find((q) => q.id === id)
    if (!item) return
    chat.removeQueued(id)
    const {
      content,
      images: qImages,
      files: qFiles,
      datasources: qRefs,
    } = item.input
    if (content) {
      draftStore.set((prev) => (prev ? `${prev}\n${content}` : content))
    }
    if (qImages?.length) {
      setImages((prev) => [...prev, ...qImages])
    }
    if (qFiles?.length) {
      setFiles((prev) => [...prev, ...qFiles.filter((f) => !prev.includes(f))])
    }
    if (qRefs?.length) {
      setDbRefs((prev) => [...prev, ...qRefs.filter((r) => !prev.includes(r))])
    }
  }

  /** 「调整方向」：把排队插话立即发出，插进正在跑的轮（steering）。 */
  function steerQueued(id: number) {
    const item = chat.queued.find((q) => q.id === id)
    if (!item) return
    chat.removeQueued(id)
    void chat.send(item.input)
  }

  /** 空态建议芯片：草稿态直接开会话，老会话正常发。 */
  function sendSuggestion(text: string) {
    if (isNew) {
      void newSession.start({ content: text })
    } else {
      void chat.send({ content: text })
    }
  }

  if (chat.loading) {
    return (
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-3 p-4 lg:p-6">
        <Skeleton className="h-16 w-2/3" />
        <Skeleton className="h-16 w-1/2 self-end" />
        <Skeleton className="h-16 w-3/4" />
      </div>
    )
  }

  // 会话已被删除或 id 无效：给明确的出口，而不是报"连不上 agent"。
  if (chat.notFound) {
    return (
      <Empty className="h-full justify-center">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <MessagesSquareIcon />
          </EmptyMedia>
          <EmptyTitle>{t("errors.sessionNotFound")}</EmptyTitle>
          <EmptyDescription>{t("errors.sessionNotFoundHint")}</EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button render={<Link to="/sessions" />}>
            {t("errors.backToSessions")}
          </Button>
        </EmptyContent>
      </Empty>
    )
  }

  const chatPanelData: ChatPanelData = {
    isNew,
    chat,
    newSession,
    draftStore,
    images,
    files,
    dbRefs,
    removeImage: (i) => setImages((prev) => prev.filter((_, idx) => idx !== i)),
    removeFile: (i) => setFiles((prev) => prev.filter((_, idx) => idx !== i)),
    removeDbRef: (i) => setDbRefs((prev) => prev.filter((_, idx) => idx !== i)),
    addDbRef,
    addImages: (picked) => void addImages(picked),
    submit,
    sendSuggestion,
    recallQueued,
    steerQueued,
    openImagePicker: () => imageInputRef.current?.click(),
    openFilePicker: () => setFilePickerOpen(true),
    openDbRefPicker: () => setDbRefPickerOpen(true),
    openUpload: () => setUploadOpen(true),
    openCwdPicker: () => setCwdPickerOpen(true),
    draftCwd,
  }

  return (
    <WorkspaceProvider
      sessionId={isNew ? 0 : sessionId}
      draftCwd={isNew ? newSession.cwd.trim() : undefined}
    >
      <ChatPanelContext.Provider value={chatPanelData}>
        {/* overflow-hidden 是必须的：dockview 内部是「视图层 + overlay 渲染层」
            两层各占 100%，scrollHeight 天然是自身高度的两倍。外层若允许滚动，
            页面底下就会多出一屏空白还能往下滚——工作区是固定视口的布局，
            滚动归各面板自己管。 */}
        <div className="min-h-0 flex-1 overflow-hidden p-1.5">
          <WorkspaceDock />
        </div>
      </ChatPanelContext.Provider>
      {/* turn 结束刷新 git/文件树——agent 改完文件的时刻。 */}
      <WorkspaceAutoRefresh busy={chat.busy} />
      {/* git 面板右键的「让 AI 分析」：直接发出去。点那一下的意图就是
          「现在分析给我看」，再让人回输入框按一次回车是多余的一步。
          草稿态还没有会话可发，退化成填进输入框。 */}
      <WorkspaceAskSink
        onAsk={(prompt) => {
          if (isNew) {
            draftStore.set((prev) =>
              prev.trim() ? `${prev.trimEnd()}\n\n${prompt}` : prompt
            )
            return
          }
          void chat.send({ content: prompt })
        }}
      />
      {/* 工作区右键/预览的「添加到引用」落到 composer 的 @ 引用列表。 */}
      <WorkspaceReferenceSink
        onAdd={(path) =>
          setFiles((prev) => (prev.includes(path) ? prev : [...prev, path]))
        }
      />

      <input
        ref={imageInputRef}
        type="file"
        accept="image/*"
        multiple
        className="hidden"
        onChange={(e) => {
          const picked = Array.from(e.target.files ?? [])
          e.target.value = ""
          if (picked.length > 0) void addImages(picked)
        }}
      />

      <DirPicker
        mode="file"
        open={filePickerOpen}
        onOpenChange={setFilePickerOpen}
        initialPath={
          (isNew ? newSession.cwd.trim() : chat.session?.cwd) || undefined
        }
        onSelect={(path) =>
          setFiles((prev) => (prev.includes(path) ? prev : [...prev, path]))
        }
      />

      {/* @ 数据库引用：选中的数据源/库/表随消息一起发出，后端查现状嵌入。
          草稿态选了工作目录就可用——项目由目录决定，不必等首条消息建会话。 */}
      <DbRefPicker
        open={dbRefPickerOpen}
        sessionId={isNew ? 0 : sessionId}
        scope={api.sessions}
        draftCwd={isNew ? newSession.cwd.trim() : undefined}
        onOpenChange={setDbRefPickerOpen}
        onSelect={addDbRef}
      />

      {/* 上传本机文件：落盘之后就是一条普通的 @ 文件引用。 */}
      <UploadDialog
        open={uploadOpen}
        onOpenChange={setUploadOpen}
        onSelect={(path) =>
          setFiles((prev) => (prev.includes(path) ? prev : [...prev, path]))
        }
      />

      {/* 草稿态：点状态栏里的工作目录换目录。 */}
      <DirPicker
        open={cwdPickerOpen}
        onOpenChange={setCwdPickerOpen}
        // 没选过就不给起点：后端从工作区根开始（owner 是设置里的那个，
        // 访客是自己的目录），而不是从 agent 记录里的历史 cwd。
        initialPath={newSession.cwd.trim() || undefined}
        onSelect={newSession.setCwd}
      />
    </WorkspaceProvider>
  )
}
