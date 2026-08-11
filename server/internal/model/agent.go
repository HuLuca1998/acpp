package model

import "time"

// AgentStatus 是本地记录的 agent 连接状态。
type AgentStatus string

const (
	AgentIdle      AgentStatus = "idle"
	AgentConnected AgentStatus = "connected"
	AgentError     AgentStatus = "error"
	AgentDisabled  AgentStatus = "disabled"
)

// Agent 是一个可通过 stdio 启动、并完成 ACP initialize 握手的进程配置。
type Agent struct {
	ID          uint        `gorm:"primaryKey" json:"id"`
	Name        string      `gorm:"size:128;not null;uniqueIndex" json:"name"`
	Description string      `gorm:"size:512" json:"description"`
	Command     string      `gorm:"size:512;not null" json:"command"`
	Args        StringSlice `gorm:"type:text" json:"args"`
	Env         StringMap   `gorm:"type:text" json:"env"`
	Cwd         string      `gorm:"size:512" json:"cwd"`
	Status      AgentStatus `gorm:"size:32;not null;default:idle;index" json:"status"`
	LastError   string      `gorm:"size:1024" json:"lastError"`
	// Flavor 与 Models 是探测缓存：注册/更新后拉一个临时会话读能力得来，
	// 供新会话页在建会话之前展示跨 agent 的模型清单。
	Flavor string          `gorm:"size:32" json:"flavor"`
	Models AgentModelSlice `gorm:"type:text" json:"models"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`

	Sessions []Session `gorm:"constraint:OnDelete:CASCADE" json:"-"`
}
