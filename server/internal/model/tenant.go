package model

import "time"

// Tenant 是一个局域网访客的身份与隔离单元（adr-007）。
//
// owner 刻意不在表内：owner 由 loopback 判定，没有记录也就没有「把自己
// 停用」这种事故，凭证也无从泄露。
type Tenant struct {
	ID uint `gorm:"primaryKey" json:"id"`
	// Name 同时是 root 目录名，因此建后不可改——改名等于搬家，会让所有
	// 已建会话的 cwd 悬空。
	Name string `gorm:"size:64;not null;uniqueIndex" json:"name"`
	// Token 既是邀请链接里的凭证，也是兑换后 cookie 的值。只对 owner 可见
	// （json 标签留空，列表接口单独带出），停用即整体失效。
	Token string `gorm:"size:64;not null;uniqueIndex" json:"-"`
	// Root 是该租户的最上层工作目录：一切路径面（目录选择、会话 cwd、
	// 文件树、终端、项目）都在此落闸。
	Root string `gorm:"size:512;not null" json:"root"`
	// Disabled 是 owner 的开关：分享链接本身不设有效期（局域网里让人隔
	// 三差五重新要链接只会招人烦），要断谁的访问就停用谁——每次鉴权现查，
	// 即时生效，会话与目录都保留。
	Disabled bool `gorm:"not null;default:false" json:"disabled"`
	// LastSeenAt 是最近一次带有效凭证发请求的时间，供 owner 判断谁还在用。
	// 每请求都写库太贵，节流到分钟级（见 service.TenantService.Authenticate）。
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}
