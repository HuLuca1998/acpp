package model

import "time"

// AgentStatus 是本地记录的 agent 连接状态。
type AgentStatus string

const (
	AgentIdle  AgentStatus = "idle"
	AgentError AgentStatus = "error"
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
	// Flavor / Models / Commands 是探测缓存：注册/更新后拉一个临时会话读
	// 能力得来，供草稿态在建会话之前展示跨 agent 模型清单与斜杠命令补全。
	Flavor   string            `gorm:"size:32" json:"flavor"`
	Models   AgentModelSlice   `gorm:"type:text" json:"models"`
	Commands AgentCommandSlice `gorm:"type:text" json:"commands"`
	// Skeleton 是模型之外的设置骨架（efforts/levels/plan/fast 支持位），
	// 与 Models 一起构成未连接会话的完整降级设置视图。
	Skeleton AgentSkeleton `gorm:"type:text" json:"skeleton"`
	// FastPolicy 是「快速模式」的使用取舍：空=未定（首次探测按 flavor 落
	// 默认——claude 因额外计费默认 off，其余默认 on），"on"/"off" 之后
	// 归用户在配置页管理，重探不覆盖。off 时快速开关不出现在任何界面。
	FastPolicy string    `gorm:"size:8" json:"fastPolicy"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`

	Sessions []Session `gorm:"constraint:OnDelete:CASCADE" json:"-"`
}
