package model

import "time"

// APILog 是一次 HTTP API 请求的记录：谁、什么时候、请求了什么、拿回什么、花多久。
//
// 它是**运行时观测，不是审计账本**：留最近若干条，头里的凭证落库前抹掉，
// 正文只留前几 KB。回答的是「刚才那个请求发生了什么」——排查前端报错、
// 看别的 AI 经 /api/ask 传了什么，够用了。
type APILog struct {
	ID     uint   `gorm:"primaryKey" json:"id"`
	Method string `gorm:"size:8;index" json:"method"`
	// Path 不含查询串；查询串单独一列，搜路径时不会被参数干扰。
	Path   string `gorm:"size:512;index" json:"path"`
	Query  string `gorm:"size:2048" json:"query"`
	Status int    `gorm:"index" json:"status"`
	// DurationMs 是 handler 从收到请求到写完响应的耗时。
	DurationMs int64 `json:"durationMs"`
	// RemoteAddr 是对方 IP（去掉端口）；Origin 是请求发自哪个页面（Origin 头，
	// 没有就取 Referer）——本机界面、局域网访客、别的 AI 的 CLI 一眼分开。
	RemoteAddr string `gorm:"size:64;index" json:"remoteAddr"`
	Origin     string `gorm:"size:256" json:"origin"`
	UserAgent  string `gorm:"size:256" json:"userAgent"`
	// Identity 是请求身份的人话：owner / 租户名 / anonymous。
	Identity string `gorm:"size:64;index" json:"identity"`
	// 头是 JSON 对象文本（键 → 值，多值用逗号并起来），凭证类已抹成 [redacted]。
	RequestHeaders  string `json:"requestHeaders"`
	ResponseHeaders string `json:"responseHeaders"`
	// 正文按字节截断保留开头；二进制正文不存，只记大小。
	RequestBody  string    `json:"requestBody"`
	ResponseBody string    `json:"responseBody"`
	RequestSize  int64     `json:"requestSize"`
	ResponseSize int64     `json:"responseSize"`
	CreatedAt    time.Time `gorm:"index" json:"createdAt"`
}
