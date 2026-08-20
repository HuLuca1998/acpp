package model

import "time"

// MCP 调用的来源。
const (
	// MCPSourceAgent 是 agent 子进程回连发起的调用——统计想看的主要是这一类。
	MCPSourceAgent = "agent"
	// MCPSourceManual 是工具台里人工试运行的调用，跟着记是为了让「这条
	// 结果是谁跑的」有答案，排查时不至于把自己刚点的那次当成 AI 干的。
	MCPSourceManual = "manual"
)

// MCPCall 是一次 MCP 工具调用的记录：谁调的、传了什么、拿回什么、花多久。
//
// 工具声明是代码，不入库；这张表只记**发生过的调用**，用于工具台的调用
// 记录与次数统计。Args/Result 存截断后的文本——一次 db_query 的返回可能
// 几百行，完整留存对「看看 AI 干了什么」没有额外价值，却会把库撑大。
type MCPCall struct {
	ID     uint   `gorm:"primaryKey" json:"id"`
	Server string `gorm:"index" json:"server"`
	Tool   string `gorm:"index" json:"tool"`
	// SessionID 为 0 表示不是会话发起的（工具台人工试运行）。
	SessionID uint `gorm:"index" json:"sessionId"`
	// Source 是 MCPSourceAgent / MCPSourceManual 之一。
	Source     string    `gorm:"index" json:"source"`
	Cwd        string    `json:"cwd"`
	Args       string    `json:"args"`
	Result     string    `json:"result"`
	IsError    bool      `json:"isError"`
	DurationMs int64     `json:"durationMs"`
	CreatedAt  time.Time `gorm:"index" json:"createdAt"`
}

// MCPToolStat 是按工具聚合的调用统计（工具台清单上的次数与健康度）。
// 不是表，是查询结果。
type MCPToolStat struct {
	Server     string    `json:"server"`
	Tool       string    `json:"tool"`
	Count      int64     `json:"count"`
	ErrorCount int64     `json:"errorCount"`
	AvgMs      int64     `json:"avgMs"`
	LastUsedAt time.Time `json:"lastUsedAt"`
}
