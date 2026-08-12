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
 */
export function MarkdownContent({
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
}
