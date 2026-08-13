package model

import "time"

// SkillUsage 是一个技能被 AI 调用的累计次数。技能本身以磁盘为事实源
// （<dataDir>/skills），不进数据库；这里只记运行时观测到的使用统计，
// 主键是技能目录名（去掉 claude 的 acpp: plugin 前缀后归一到目录名）。
type SkillUsage struct {
	Name       string    `gorm:"primaryKey" json:"name"`
	Count      int64     `json:"count"`
	LastUsedAt time.Time `json:"lastUsedAt"`
}
