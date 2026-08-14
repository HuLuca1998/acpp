import { memo, useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import type { IDockviewPanelProps } from "dockview-react"
import { Terminal } from "@xterm/xterm"
import { FitAddon } from "@xterm/addon-fit"
import "@xterm/xterm/css/xterm.css"
import { TerminalIcon } from "lucide-react"

import { api } from "@/lib/api"
import { useWorkspace } from "@/components/workspace/workspace-context"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"

type TermStatus = "boot" | "live" | "exited"

/** 把语义 token 解析成 xterm 能吃的具体色值（oklch → rgb 由浏览器代劳）。 */
function resolveColor(varName: string, fallback: string): string {
  const probe = document.createElement("div")
  probe.style.color = `var(${varName})`
  probe.style.display = "none"
  document.body.appendChild(probe)
  const resolved = getComputedStyle(probe).color
  probe.remove()
  return resolved || fallback
}

/**
 * 终端面板：真 PTY。首次可见才 spawn（惰性激活）；ws 断线由后端保活
 * 30s 供刷新重连；pty 退出显示重启空态。面板用 renderer:always 挂载，
 * tab 切走 DOM 保留、scrollback 不丢。
 */
export const TerminalPanel = memo(function TerminalPanel(
  props: IDockviewPanelProps
) {
  const { t } = useTranslation()
  const ws = useWorkspace()
  const hostRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const sockRef = useRef<WebSocket | null>(null)
  const [status, setStatus] = useState<TermStatus>("boot")
  const [termId, setTermId] = useState<string>(
    (props.params as { termId?: string })?.termId ?? ""
  )
  const [visible, setVisible] = useState(props.api.isVisible)

  useEffect(() => {
    const d = props.api.onDidVisibilityChange((e) => setVisible(e.isVisible))
    return () => d.dispose()
  }, [props.api])

  // 首次可见且还没有 pty：现场 spawn 一个并把 termId 写回面板参数
  //（进布局序列化，刷新后凭它重连）。
  const spawnedRef = useRef(false)
  useEffect(() => {
    if (!visible || termId || !ws.sessionId || spawnedRef.current) return
    spawnedRef.current = true
    api.sessions
      .terminalCreate(ws.sessionId)
      .then((info) => {
        props.api.updateParameters({ termId: info.id, num: info.num })
        setTermId(info.id)
      })
      .catch(() => setStatus("exited"))
  }, [visible, termId, ws.sessionId, props.api])

  // xterm 实例与 ws 连接的生命周期：termId 变化（重启）时整体重建。
  useEffect(() => {
    const host = hostRef.current
    if (!termId || !ws.sessionId || !host) return

    const term = new Terminal({
      fontFamily:
        "ui-monospace, SFMono-Regular, Menlo, Monaco, 'Courier New', monospace",
      fontSize: 12,
      lineHeight: 1.25,
      cursorBlink: true,
      scrollback: 5000,
      theme: {
        // xterm 用 canvas 渲染，不认 CSS 变量；运行时解析 token，hex 只是解析失败的兜底。
        background: resolveColor("--card", "#1e1e1e"), // check-ignore: xterm canvas 兜底色
        foreground: resolveColor("--foreground", "#d4d4d4"), // check-ignore: xterm canvas 兜底色
        cursor: resolveColor("--primary", "#7aa2f7"), // check-ignore: xterm canvas 兜底色
        // 选区要半透明，token 是实色，只能用中性灰罩层。
        selectionBackground: "rgba(128, 128, 128, 0.35)", // check-ignore: xterm 选区罩层
      },
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(host)
    fit.fit()
    termRef.current = term
    fitRef.current = fit

    const sock = new WebSocket(ws.scope.terminalWsUrl(ws.sessionId, termId))
    sock.binaryType = "arraybuffer"
    sockRef.current = sock
    const encoder = new TextEncoder()

    sock.onopen = () => {
      setStatus("live")
      sock.send(
        JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows })
      )
      term.focus()
    }
    sock.onmessage = (e) => {
      term.write(new Uint8Array(e.data as ArrayBuffer))
    }
    sock.onclose = () => setStatus("exited")

    const dataSub = term.onData((d) => {
      if (sock.readyState === WebSocket.OPEN) sock.send(encoder.encode(d))
    })

    // resize：节流 fit（拖分割线中不逐帧 reflow 终端），终值由最后一次触发。
    let fitTimer: ReturnType<typeof setTimeout> | null = null
    const scheduleFit = () => {
      if (fitTimer) clearTimeout(fitTimer)
      fitTimer = setTimeout(() => {
        if (!host.isConnected || host.clientWidth === 0) return
        fit.fit()
        if (sock.readyState === WebSocket.OPEN) {
          sock.send(
            JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows })
          )
        }
      }, 150)
    }
    const observer = new ResizeObserver(scheduleFit)
    observer.observe(host)

    return () => {
      observer.disconnect()
      if (fitTimer) clearTimeout(fitTimer)
      dataSub.dispose()
      sock.onclose = null
      sock.close()
      term.dispose()
      termRef.current = null
      sockRef.current = null
    }
  }, [termId, ws.sessionId, ws.scope])

  // tab 切回来时补一次 fit（隐藏期间的尺寸变化 ResizeObserver 可能测不到）。
  useEffect(() => {
    if (visible && termRef.current) {
      fitRef.current?.fit()
      termRef.current.focus()
    }
  }, [visible])

  const restart = useCallback(() => {
    if (!ws.sessionId) return
    setStatus("boot")
    api.sessions
      .terminalCreate(ws.sessionId)
      .then((info) => {
        props.api.updateParameters({ termId: info.id, num: info.num })
        setTermId(info.id)
      })
      .catch(() => setStatus("exited"))
  }, [ws.sessionId, props.api])

  if (!ws.sessionId) {
    return (
      <Empty className="h-full justify-center">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <TerminalIcon />
          </EmptyMedia>
          <EmptyTitle className="text-sm">
            {t("workspace.tree.draftTitle")}
          </EmptyTitle>
          <EmptyDescription className="text-xs">
            {t("workspace.tree.draftHint")}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className="relative h-full [contain:strict]">
      <div ref={hostRef} className="h-full p-2" />
      {status === "exited" ? (
        <Empty className="absolute inset-0 justify-center bg-card">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <TerminalIcon />
            </EmptyMedia>
            <EmptyTitle className="text-sm">
              {t("workspace.terminal.exitedTitle")}
            </EmptyTitle>
            <EmptyDescription className="text-xs">
              {t("workspace.terminal.exitedHint")}
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button size="sm" variant="outline" onClick={restart}>
              {t("workspace.terminal.restart")}
            </Button>
          </EmptyContent>
        </Empty>
      ) : null}
    </div>
  )
})
