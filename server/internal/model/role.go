package model

import "time"

// Role 是编排里可雇佣的子代理角色（adr-006）：人格提示词 + 绑定工具 +
// 模型/思考深度/权限档预设。全局资产，编排主会话按它拉起子会话。
type Role struct {
	ID   uint   `gorm:"primaryKey" json:"id"`
	Name string `gorm:"size:128;not null;uniqueIndex" json:"name"`
	// Description 是职责简介，会进主会话的调度提示词（雇佣目录），
	// 主会话据此决定派活给谁——写给 AI 看，要说清"什么活找它"。
	Description string `gorm:"size:1024" json:"description"`
	// Persona 是注入子会话的角色设定（claude 走 _meta.systemPrompt.append，
	// codex 走角色专属 CODEX_HOME 的 AGENTS.md）。
	Persona string `gorm:"type:text" json:"persona"`
	// AgentID 绑定承载该角色的工具（claude/codex 内置记录之一）。
	AgentID uint `gorm:"not null;index" json:"agentId"`
	// Model / Effort / Level 是统一设置预设：空值 = 沿用该工具的默认档。
	Model  string `gorm:"size:128" json:"model"`
	Effort string `gorm:"size:32" json:"effort"`
	Level  string `gorm:"size:32" json:"level"`
	// Builtin 标记启动时预置的模板角色——可改内容，删除后下次启动不复活
	//（按 name 判存，与 Agent 预置同策略）。
	Builtin   bool      `json:"builtin"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	Agent *Agent `gorm:"foreignKey:AgentID" json:"-"`
}
