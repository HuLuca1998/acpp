package model

import "time"

// SessionState 是会话的生命周期状态。
type SessionState string

const (
	SessionActive SessionState = "active"
	SessionIdle   SessionState = "idle"
	SessionEnded  SessionState = "ended"
	SessionError  SessionState = "error"
)

// Session 对应 ACP 的一次 session/new，是消息流的容器。
type Session struct {
	ID uint `gorm:"primaryKey" json:"id"`
	// AgentID 指向发起该会话的 agent。
	AgentID uint `gorm:"not null;index" json:"agentId"`
	// TenantID 是会话归属的租户（adr-007）；0 表示 owner 自己的会话。
	// 隔离靠查询条件执行，不靠 handler 自觉——见 service.Scope。
	TenantID uint `gorm:"not null;default:0;index" json:"tenantId"`
	// ACPSessionID 是 agent 侧返回的 sessionId，用于 session/prompt 等后续调用。
	ACPSessionID string       `gorm:"size:128;index" json:"acpSessionId"`
	Title        string       `gorm:"size:256" json:"title"`
	Cwd          string       `gorm:"size:512" json:"cwd"`
	State        SessionState `gorm:"size:32;not null;default:active;index" json:"state"`
	// StopReason 记录 session/prompt 的返回原因，如 end_turn、max_tokens、cancelled。
	StopReason string `gorm:"size:64" json:"stopReason"`
	// MessageCount 是重建后的消息数缓存：turn 结束时写回，列表读取 O(1)——
	// 别在列表路径上做全量转录重建。
	MessageCount int `json:"messageCount"`
	// LastSettings 是最后一次生效的统一设置当前值快照
	// （model/effort/level/plan/fast），恢复会话的降级视图用它填 Current*，
	// 让三种状态（新建/进行中/恢复）的设置控件显示一致。
	LastSettings JSONMap `gorm:"type:text" json:"lastSettings,omitempty"`
	// LastUsage 是最后一次上报的用量快照（used/size/cost）。
	//
	// 上下文水位只经 usage_update 通知流过，是彻头彻尾的事件态：会话一停、
	// 页面一刷新就没了。可占用比例正是「这条会话还剩多少余量」这个问题的
	// 答案，不该只在轮内可见——所以按 LastSettings 同款存快照，未连接的
	// 会话也能显示最近的数字。轮末写一次，不跟着每条通知抖。
	LastUsage JSONMap `gorm:"type:text" json:"lastUsage,omitempty"`
	// MCPToken 是本会话专属 MCP 端点的路径令牌（/api/mcp/db/{token}）：
	// agent 子进程带着它回连拿数据库工具。懒生成——只有真的挂载了工具面
	// 的会话才有值。不出 API：它是凭证。
	//
	// 索引刻意不加 unique：绝大多数会话这里是空串，而 SQLite 的唯一索引
	// 会把多个空串判成重复。唯一性由 24 字节随机数保证，不靠约束。
	MCPToken  string    `gorm:"size:64;index" json:"-"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `gorm:"index" json:"updatedAt"`

	Agent *Agent `gorm:"foreignKey:AgentID" json:"-"`
}
