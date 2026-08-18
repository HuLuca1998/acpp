package model

import "time"

// DataSource 是一个外部 MySQL 数据源的连接配置。
//
// 身份是「项目 + 环境」两级（`pp-game` 的 `local` / `dev` / `pre`），不是
// 一个自由命名的连接——同一个项目的几套环境天然要放在一起看、一起切，
// 用两个字段表达比让用户在名字里编码约定可靠。二者组合唯一，
// `<项目>/<环境>` 就是它对外的标识（AI 调 MCP 工具时填的 source）。
//
// 只存配置不存连接：每次查询都是「拨号 → 执行 → 关闭」的一次性连接
// （含 SSH 隧道），因此这张表里没有任何运行态字段——面板重启、隧道断掉
// 都不需要对账。
//
// 读写边界有两道：数据源上的 ReadOnly 开关（软件层闸门，见其注释）与
// 连接账号的授权范围（真正的边界）。前者让「这条连接只用来查」成为一个
// 可配置的事实，后者保证就算闸门被绕过也改不动东西——要绝对安全，
// 就给这条连接配一个只有 SELECT 的账号。
type DataSource struct {
	ID uint `gorm:"primaryKey" json:"id"`
	// Project 是数据源归属的项目，建议与工作区项目同名（`pp-game`），
	// 但不做外键校验：数据库先于代码存在是常态，不能因为本机还没 clone
	// 就配不了连接。
	Project string `gorm:"size:128;not null;uniqueIndex:idx_ds_project_env" json:"project"`
	// Env 是环境标签（local / dev / pre / prod 之类），自由文本。
	Env  string `gorm:"size:64;not null;uniqueIndex:idx_ds_project_env" json:"env"`
	Host string `gorm:"size:256;not null" json:"host"`
	Port int    `gorm:"not null;default:3306" json:"port"`
	User string `gorm:"size:128;not null" json:"user"`
	// Password 永不出 API：响应里只给 HasPassword 这类布尔位，
	// 编辑时留空表示不修改。
	Password string `gorm:"size:512" json:"-"`
	// Database 是默认库，可空（空则连上后再按需选库）。
	Database string `gorm:"size:128" json:"database"`
	// Databases 是这个数据源允许访问的库，逗号分隔。
	//
	// 空表示**沿用 Database**——配了默认库就只看那一个。这是有意的默认：
	// 一个账号常常能连到整台实例上的全部库，而用户配数据源时心里想的
	// 是「这个项目的库」，不是「这台机器上的所有库」。要跨库就把库都
	// 列在这里，或者填 `*` 显式放开。
	//
	// **这是收窄视野，不是安全边界**：明写 `别的库.表` 的 SQL 会被挡，
	// 但动态 SQL、存储过程绕得过去。真正的边界是连接账号的授权范围。
	Databases string `gorm:"size:512" json:"databases"`
	// Params 是追加到 DSN 的额外参数（如 `charset=utf8mb4&tls=skip-verify`）。
	Params string `gorm:"size:512" json:"params"`
	Note   string `gorm:"size:512" json:"note"`

	// SSH 隧道：开启后先连跳板机，再从跳板机拨 MySQL——所以 Host/Port 是
	// **跳板机视角**的地址，线上库多半填 127.0.0.1:3306。
	SSHEnabled bool   `gorm:"not null;default:false" json:"sshEnabled"`
	SSHHost    string `gorm:"size:256" json:"sshHost"`
	SSHPort    int    `gorm:"not null;default:22" json:"sshPort"`
	SSHUser    string `gorm:"size:128" json:"sshUser"`
	// SSHAuth 是验证方式：password / key / both（照 Navicat 的三选一）。
	// 做成显式选择而不是「填了哪个用哪个」：两种凭证都留着、但这次只想用
	// 公钥，是很常见的诉求，靠猜实现不了。
	SSHAuth     string `gorm:"size:16;not null;default:password" json:"sshAuth"`
	SSHPassword string `gorm:"size:512" json:"-"`
	// SSHKeyPath 是私钥文件路径（不把私钥内容搬进库，权限跟着文件系统走）。
	SSHKeyPath    string `gorm:"size:512" json:"sshKeyPath"`
	SSHPassphrase string `gorm:"size:512" json:"-"`

	// ReadOnly 决定这条连接能不能改数据：只读时软件层拒绝执行写语句
	// （INSERT/UPDATE/DELETE/DDL 等）。默认开——新配的连接先按不能写
	// 对待，要写是明确的一次决定。
	//
	// **这是闸门不是边界**：它按首关键字判断语句类型，挡不住存储过程里
	// 的写操作、动态拼出来再执行的 SQL、以及有副作用的函数。真正的边界
	// 仍是连接账号的授权范围——要绝对不可写，就配一个只有 SELECT 的账号。
	ReadOnly bool `gorm:"not null;default:true" json:"readOnly"`

	// Disabled 的数据源不会挂进会话的 MCP 工具面，页面里仍可编辑。
	Disabled  bool      `gorm:"not null;default:false" json:"disabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `gorm:"index" json:"updatedAt"`

	// 以下不入库。Ref 是 `<项目>/<环境>` 派生标识；三个 Has* 是给前端表单的
	// 「有没有配」标志位——密码本身不出 API，但界面必须能区分「没设密码」
	// 与「设了但看不见」。
	Ref              string `gorm:"-" json:"ref"`
	HasPassword      bool   `gorm:"-" json:"hasPassword"`
	HasSSHPassword   bool   `gorm:"-" json:"hasSSHPassword"`
	HasSSHPassphrase bool   `gorm:"-" json:"hasSSHPassphrase"`
}
