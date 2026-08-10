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
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`

	Sessions []Session `gorm:"constraint:OnDelete:CASCADE" json:"-"`
}
