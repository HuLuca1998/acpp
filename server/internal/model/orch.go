package model

import "time"

// OrchSession 是编排主会话（adr-006）：挂载系统 MCP、可雇佣角色子代理的
// 对话。与普通 Session 刻意分表——隔离契约要求编排功能整体可删而不影响
// 普通会话。生命周期状态复用 SessionState 词汇。
type OrchSession struct {
	ID uint `gorm:"primaryKey" json:"id"`
	// AgentID 是主会话使用的工具（claude/codex）。
	AgentID      uint         `gorm:"not null;index" json:"agentId"`
	ACPSessionID string       `gorm:"size:128;index" json:"acpSessionId"`
	Title        string       `gorm:"size:256" json:"title"`
	Cwd          string       `gorm:"size:512" json:"cwd"`
	State        SessionState `gorm:"size:32;not null;default:idle;index" json:"state"`
	StopReason   string       `gorm:"size:64" json:"stopReason"`
	MessageCount int          `json:"messageCount"`
	LastSettings JSONMap      `gorm:"type:text" json:"lastSettings,omitempty"`
	// MCPToken 是本会话专属 MCP 端点的路径令牌（/api/mcp/{token}）：
	// 既标识哪条主会话在派活，也保证子会话拿不到派活能力（深度 1）。
	MCPToken string `gorm:"size:64;uniqueIndex" json:"-"`
	// 累计 token 用量（主会话自身 + 全部子任务），turn/任务收尾时累加。
	TokensUsed int64     `json:"tokensUsed"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `gorm:"index" json:"updatedAt"`

	Agent *Agent `gorm:"foreignKey:AgentID" json:"-"`
}

// OrchTaskState 是子任务的生命周期状态。
type OrchTaskState string

const (
	// OrchTaskRunning 表示子会话正在跑这个任务。
	OrchTaskRunning OrchTaskState = "running"
	// OrchTaskDone 表示任务正常完成（end_turn）。
	OrchTaskDone OrchTaskState = "done"
	// OrchTaskFailed 表示任务失败或非正常结束（错误、超时、中止）。
	OrchTaskFailed OrchTaskState = "failed"
)

// OrchTask 是一次 spawn_agent 派发：一个任务 = 一条角色子会话。
// 记录本身就是「派发流」的数据源——谁派给谁、派了什么、结果如何。
type OrchTask struct {
	ID            uint `gorm:"primaryKey" json:"id"`
	OrchSessionID uint `gorm:"not null;index" json:"orchSessionId"`
	// RoleID 可能指向已被删除的角色，展示字段冗余存一份。
	RoleID   uint   `gorm:"index" json:"roleId"`
	RoleName string `gorm:"size:128" json:"roleName"`
	// Task 是主会话派发的任务原文。
	Task         string        `gorm:"type:text" json:"task"`
	ACPSessionID string        `gorm:"size:128;index" json:"acpSessionId"`
	State        OrchTaskState `gorm:"size:32;not null;default:running;index" json:"state"`
	StopReason   string        `gorm:"size:64" json:"stopReason"`
	// Result 是子会话的最终回复（返还给主会话的工具结果原文）。
	Result       string `gorm:"type:text" json:"result"`
	MessageCount int    `json:"messageCount"`
	// TokensUsed 是子会话累计 token（turn_end 计量的两端交集字段）。
	TokensUsed int64 `json:"tokensUsed"`
	// DurationMS 是任务耗时（毫秒），收尾时写。
	DurationMS int64     `json:"durationMs"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `gorm:"index" json:"updatedAt"`

	OrchSession *OrchSession `gorm:"foreignKey:OrchSessionID;constraint:OnDelete:CASCADE" json:"-"`
}
