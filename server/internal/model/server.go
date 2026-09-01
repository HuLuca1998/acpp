package model

import "time"

// Server 是一台可以 SSH 连上去的机器。
//
// 它同时是两件事的底座：**观察目标**（AI 用只读工具看它的文件、容器与
// 负载）与**拨号跳板**（数据源经它连到内网的 MySQL）。这两件事本来就是
// 同一台机器，分开各存一份凭证的话，改个端口要改两处，而且没有任何一处
// 知道它们是同一台——所以 DataSource 的 SSH 配置改成引用这里（ServerID）。
//
// 身份是一个自由命名的 Name（唯一），不像数据源那样分「项目 + 环境」两级：
// 服务器**刻意不做项目隔离**（adr-019）。一台机器上跑着多个项目是常态，
// 按项目切会把 AI 需要的上下文一起切掉。可见性的代价写在文档里：配置一台
// 服务器，就等于授权 AI 观察整台机器。
//
// 只存配置不存连接：每次调用都是「拨号 → 执行 → 关闭」的一次性连接，
// 所以这张表里没有任何运行态字段。
type Server struct {
	ID uint `gorm:"primaryKey" json:"id"`
	// Name 是唯一标识，AI 调工具时填的就是它（`pp-game-live`）。
	Name string `gorm:"size:128;not null;uniqueIndex" json:"name"`
	Host string `gorm:"size:256;not null" json:"host"`
	Port int    `gorm:"not null;default:22" json:"port"`
	User string `gorm:"size:128;not null" json:"user"`
	// Auth 是验证方式：password / key / both（照 Navicat 的三选一）。
	// 做成显式选择而不是「填了哪个用哪个」：两种凭证都留着、但这次只想
	// 用公钥，是很常见的诉求，靠猜实现不了。
	Auth string `gorm:"size:16;not null;default:password" json:"auth"`
	// Password / Passphrase 永不出 API：响应里只给 Has* 布尔位，
	// 编辑时留空表示不修改。
	Password string `gorm:"size:512" json:"-"`
	// KeyPath 是私钥文件路径（不把私钥内容搬进库，权限跟着文件系统走）。
	// key/both 档下留空则走 ssh-agent。
	KeyPath    string `gorm:"size:512" json:"keyPath"`
	Passphrase string `gorm:"size:512" json:"-"`
	// Note 是用途说明，**会随工具清单给 AI 看**——「pp-game 生产机，
	// 项目在 /srv/pp-game-live」这种一句话，能省掉 AI 好几轮摸索。
	Note string `gorm:"size:512" json:"note"`
	// Disabled 的服务器不挂进会话的 MCP 工具面，页面里仍可编辑。
	Disabled  bool      `gorm:"not null;default:false" json:"disabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `gorm:"index" json:"updatedAt"`

	// 以下不入库：给前端表单的「有没有配」标志位——凭证本身不出 API，
	// 但界面必须能区分「没设」与「设了但看不见」。
	HasPassword   bool `gorm:"-" json:"hasPassword"`
	HasPassphrase bool `gorm:"-" json:"hasPassphrase"`
}
