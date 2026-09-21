package model

import "time"

// SSHKey 是一把可复用的 SSH 私钥。
//
// 为什么私钥内容入库、而不是像原来那样只记文件路径：路径只在**这台**机器
// 上有意义，换台电脑就指向一个不存在的文件。要让连接配置能整套搬走，私钥
// 得跟着走。
//
// 为什么是独立一张表、而不是每台服务器各存一份：一把钥匙开好几台机器是
// 常态（同一批机器多半用同一把部署钥匙），复制几份的话换钥匙要改几处，
// 而且没有任何一处知道它们是同一把。服务器改成引用这里（KeyID）。
//
// Fingerprint / PublicKey 在保存时从私钥算出来：前者让人在下拉里认出是哪
// 把（名字可以起得很随意，指纹不会），后者能直接复制去装进目标机器的
// authorized_keys——省掉「私钥在这儿、公钥在哪儿」的来回。
type SSHKey struct {
	ID uint `gorm:"primaryKey" json:"id"`
	// Name 是唯一标识，服务器表单的下拉里显示的就是它。
	Name string `gorm:"size:128;not null;uniqueIndex" json:"name"`
	// PrivateKey 是 PEM 私钥内容，**永不出常规 API**——与密码同级，
	// 只在 owner 显式导出连接配置时带出来。
	PrivateKey string `gorm:"type:text;not null" json:"-"`
	// Passphrase 是私钥的通行短语，同样永不出常规 API。
	Passphrase string `gorm:"size:512" json:"-"`
	// Fingerprint 是 SHA256 指纹（`SHA256:...`），保存时算出来。
	Fingerprint string `gorm:"size:128" json:"fingerprint"`
	// PublicKey 是 authorized_keys 那一行，保存时从私钥导出。
	PublicKey string `gorm:"type:text" json:"publicKey"`
	// Note 是用途说明（「部署机通用钥匙」这种一句话）。
	Note      string    `gorm:"size:512" json:"note"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `gorm:"index" json:"updatedAt"`

	// 以下不入库：给界面的标志位与依赖计数。
	HasPassphrase bool `gorm:"-" json:"hasPassphrase"`
	// UsedBy 是用这把钥匙的服务器台数。列表里显示出来，删之前才看得见
	// 会影响谁。
	UsedBy int64 `gorm:"-" json:"usedBy"`
}
