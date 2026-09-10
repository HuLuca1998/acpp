package model

import "time"

// GithubWatch 是一个身份关注的 GitHub 仓库清单：GitHub 页只汇总这些仓库
// 的 issue。每个身份一行，TenantID 为 0 是 owner——owner 不入 tenants 表
// （adr-007），但他的关注清单同样要落库，所以键不能是外键。
type GithubWatch struct {
	ID        uint        `gorm:"primaryKey" json:"-"`
	TenantID  uint        `gorm:"not null;uniqueIndex" json:"-"`
	Repos     StringSlice `gorm:"type:text;not null" json:"repos"`
	UpdatedAt time.Time   `json:"updatedAt"`
}
