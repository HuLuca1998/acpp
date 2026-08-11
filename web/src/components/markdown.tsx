import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"

import { cn } from "@/lib/utils"

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
        "prose-pre:border prose-pre:border-border prose-pre:bg-background",
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
        }}
      >
        {children}
      </ReactMarkdown>
    </div>
  )
}
