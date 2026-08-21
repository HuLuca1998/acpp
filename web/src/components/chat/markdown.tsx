import { memo, useMemo } from "react"
import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"

import { cn } from "@/lib/utils"
import { CopyButton } from "@/components/chat/copy-button"

/** 递归拍平 react 子树里的文本，供代码块复制用。 */
function extractText(node: React.ReactNode): string {
  if (node == null || typeof node === "boolean") return ""
  if (typeof node === "string" || typeof node === "number") return String(node)
  if (Array.isArray(node)) return node.map(extractText).join("")
  if (typeof node === "object" && "props" in node) {
    return extractText(
      (node as { props: { children?: React.ReactNode } }).props.children
    )
  }
  return ""
}

/**
 * 围栏代码块：语言标签 + 复制按钮的头部条，内容区横向滚动。
 * 用 not-prose 脱离 prose 的 pre 样式，自己控制完整外观。
 */
function CodeBlock({ children }: { children?: React.ReactNode }) {
  const codeEl = children as
    { props?: { className?: string; children?: React.ReactNode } } | undefined
  const lang =
    /language-([\w-]+)/.exec(codeEl?.props?.className ?? "")?.[1] ?? "text"
  const text = extractText(codeEl?.props?.children ?? null).replace(/\n$/, "")

  return (
    <div className="not-prose my-3 overflow-hidden rounded-lg border border-border bg-background/50">
      <div className="flex items-center justify-between border-b border-border bg-muted/50 py-1 pr-1 pl-3">
        <span className="font-mono text-xs text-muted-foreground">{lang}</span>
        <CopyButton text={text} />
      </div>
      <pre className="overflow-x-auto p-3 font-mono text-xs leading-5">
        {codeEl as React.ReactNode}
      </pre>
    </div>
  )
}

/**
 * 渲染 agent 输出的 markdown 正文。
 * prose 配色映射到主题变量（见 index.css），深浅色主题都一致。
 * memo：解析是渲染路径上最贵的一步，文本没变就不该重来一遍。
 */
export const MarkdownContent = memo(function MarkdownContent({
  children,
  className,
}: {
  children: string
  className?: string
}) {
  return (
    <div
      className={cn(
        "prose prose-sm max-w-none wrap-break-word",
        // prose-sm 默认的 ol 编号区放不下两位数序号，溢出会被消息条的
        // content-visibility（paint containment）裁掉半个数字。
        "prose-ol:ps-8",
        "prose-code:before:content-none prose-code:after:content-none",
        className
      )}
    >
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          a: (props) => <a {...props} target="_blank" rel="noreferrer" />,
          table: (props) => (
            <div className="overflow-x-auto">
              <table {...props} />
            </div>
          ),
          pre: (props) => <CodeBlock>{props.children}</CodeBlock>,
        }}
      >
        {children}
      </ReactMarkdown>
    </div>
  )
})

/**
 * 把流式文本切成「已定稿的段落块 + 活跃尾段」。切点只选在围栏代码块
 * 之外的空行上——流式是纯追加，空行之前的内容不会再变，按块渲染就能
 * 让 memo 挡住已定稿部分的重复解析。
 */
function splitStableBlocks(text: string): { blocks: string[]; tail: string } {
  const blocks: string[] = []
  let blockStart = 0
  let inFence = false
  let i = 0
  while (i < text.length) {
    const lineEnd = text.indexOf("\n", i)
    if (lineEnd === -1) break
    const line = text.slice(i, lineEnd)
    // 围栏可缩进至多 3 空格；再深就是缩进代码块里的字面 ```，不算围栏。
    if (/^ {0,3}(```|~~~)/.test(line)) {
      inFence = !inFence
    } else if (!inFence && line.trim() === "") {
      const chunk = text.slice(blockStart, lineEnd + 1)
      // 连续空行攒进下一块的开头，空白不值得独立成块。
      if (chunk.trim() !== "") {
        blocks.push(chunk)
        blockStart = lineEnd + 1
      }
    }
    i = lineEnd + 1
  }
  return { blocks, tail: text.slice(blockStart) }
}

/**
 * 流式正文渲染。整篇每个分片都全量重解析的成本随文本线性上涨（一轮长
 * 回答等于 O(n²) 的解析总量），是流式期间最大的 CPU 开销；分块后只有
 * 活跃尾段重解析。分块渲染与整篇渲染在个别边角（跨空行的嵌套列表、
 * 引用式链接定义）有细微差异，轮结束后正文由重建消息整篇渲染接管，
 * 不留痕。块间距 gap-4 对齐 prose-sm 的段落间距。
 */
export function StreamingMarkdown({ children }: { children: string }) {
  const { blocks, tail } = useMemo(
    () => splitStableBlocks(children),
    [children]
  )
  return (
    <div className="flex flex-col gap-4">
      {blocks.map((block, index) => (
        <MarkdownContent key={index}>{block}</MarkdownContent>
      ))}
      {tail.trim() !== "" ? <MarkdownContent>{tail}</MarkdownContent> : null}
    </div>
  )
}
