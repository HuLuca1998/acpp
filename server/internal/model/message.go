package model

import "time"

// MessageRole 标记消息的来源方。
type MessageRole string

const (
	RoleUser  MessageRole = "user"
	RoleAgent MessageRole = "agent"
)

// MessageKind 对应 ACP session/update 推送的各类内容块。
type MessageKind string

const (
	KindText     MessageKind = "text"
	KindThought  MessageKind = "thought"
	KindToolCall MessageKind = "tool_call"
	// KindElicitation 是一次已完成的交互式提问：问题 schema + 用户作答。
	KindElicitation MessageKind = "elicitation"
)

// Message 是会话中的一条记录，文本放 Content，结构化内容放 Payload。
type Message struct {
	ID        uint        `gorm:"primaryKey" json:"id"`
	SessionID uint        `gorm:"not null;index" json:"sessionId"`
	Role      MessageRole `gorm:"size:16;not null" json:"role"`
	Kind      MessageKind `gorm:"size:32;not null;default:text" json:"kind"`
	Content   string      `gorm:"type:text" json:"content"`
	// Payload 承载 tool_call 的入参、tool_result 的输出、plan 的条目等。
	Payload   JSONMap   `gorm:"type:text" json:"payload"`
	CreatedAt time.Time `json:"createdAt"`

	Session *Session `gorm:"foreignKey:SessionID" json:"-"`
}
