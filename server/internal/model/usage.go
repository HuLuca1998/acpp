package model

import "time"

// 成本的来源档位（TokenUsage.CostSource）。
const (
	// CostReported 是 agent 自己算好的费用（claude 经 usage_update 报
	// 累计值）。它不依赖模型识别，也不会随单价表过期，是权威口径。
	CostReported = "reported"
	// CostEstimated 是按模型单价表折算的（codex 一分钱都不报，但模型 id
	// 是具体的）。界面上必须与实报分开显示。
	CostEstimated = "estimated"
	// CostNone 是既没实报、模型又不在单价表里（升级后 id 会改）。
	// **不按零算**——零和「不知道」在账目上是两件事。
	CostNone = "none"
)

// 会话来源（TokenUsage.Origin），与 Session.Origin 同一套取值，空串统一
// 归到 OriginUI 便于按列分组。
const (
	OriginUI = "ui"
)

// TokenUsage 是一轮对话的用量账目：一轮一行，只存计量不存正文。
//
// 事实源仍然是转录 JSONL——每轮的 token 本来就写在 session/prompt 的响应
// 里，这张表只是把它读出来建的索引，随时可以照转录重算（见
// service.UsageService.Backfill）。因此两条设计：
//
//   - **不做外键**。会话删了账目还在：实测有 47 份转录的会话记录早就没了，
//     那些钱照样花过，账不该跟着入口消失。
//   - **四项 token 分开存**。实测缓存读占全部 token 的 96%，而它的单价只有
//     普通输入的 1/10——合成一个 totalTokens 就再也说不清钱花在哪。
type TokenUsage struct {
	ID uint `gorm:"primaryKey" json:"id"`
	// SessionID 是会话号码而非外键，理由见上。
	SessionID uint `gorm:"not null;index:idx_usage_turn,unique,priority:1" json:"sessionId"`
	// TurnSeq 是这一轮在会话里的序号（从 1 起）。与 SessionID 组成唯一键，
	// 回填因此天然幂等——重扫几次转录，结果都是同一批行。
	TurnSeq int `gorm:"not null;index:idx_usage_turn,unique,priority:2" json:"turnSeq"`

	// TenantID 是归属身份，0 = owner（与 Session 同一约定）。隔离条件就
	// 写在这一列上，与 StartedAt 组成主查询路径的复合索引。
	TenantID uint `gorm:"not null;default:0;index:idx_usage_tenant_time,priority:1" json:"tenantId"`
	AgentID  uint `gorm:"not null;default:0" json:"agentId"`
	// Flavor 是 runtime 方言（claude / codex / generic）。分组按它，不按
	// AgentID——agent 记录可以被改名重建，方言不会变。
	Flavor string `gorm:"size:32;index" json:"flavor"`
	// Model 是这一轮生效的模型或档位名。claude 实测有 89% 的轮次只报
	// "default"，所以模型维度只能到档位粒度，**钱不靠它算**。
	Model string `gorm:"size:64" json:"model"`
	// Project 是 `<组织>/<仓库>`，写入时按 cwd 定格；推不出就留空
	// （会话可以开在任意目录，那时它不属于任何项目）。
	Project string `gorm:"size:128;index:idx_usage_project_time,priority:1" json:"project"`
	// Cwd 留着原文：项目名的推导规则将来变了，靠它能重算出新答案。
	Cwd string `gorm:"size:512" json:"cwd"`
	// Origin 是谁发起的这一轮（ui / ask / discord / cron）。
	Origin string `gorm:"size:16;index" json:"origin"`

	// StartedAt 是 prompt 发出的时刻，所有按时间的聚合都走它。
	StartedAt  time.Time `gorm:"index:idx_usage_tenant_time,priority:2;index:idx_usage_project_time,priority:2" json:"startedAt"`
	EndedAt    time.Time `json:"endedAt"`
	DurationMs int64     `json:"durationMs"`

	InputTokens      int `json:"inputTokens"`
	OutputTokens     int `json:"outputTokens"`
	CacheReadTokens  int `json:"cacheReadTokens"`
	CacheWriteTokens int `json:"cacheWriteTokens"`
	ThoughtTokens    int `json:"thoughtTokens"`
	// TotalTokens 取 agent 报的总数，不由四项相加得出——两者对不上时
	// 以 agent 为准，它才知道自己怎么计的。
	TotalTokens int `json:"totalTokens"`

	// CostMicro 是**本轮**成本，单位百万分之一美元。
	//
	// 用整数不用浮点：几万行 float64 累加出来的合计会带一串误差尾巴，
	// 账目不接受「差不多」。1 micro = 1e-6 USD，int64 够存到天文数字。
	CostMicro int64 `json:"costMicro"`
	// CostCumMicro 是 agent 报的**会话累计**原值，只为下一轮算差分留的。
	// claude 的 cost 是单调递增的累计量，本轮成本 = 本次累计 - 上次累计；
	// 跨 session/load 恢复后可能重置，那时差分为负，按「新起点」处理。
	CostCumMicro int64 `json:"-"`
	// CostSource 是 CostReported / CostEstimated / CostNone 之一。
	CostSource string `gorm:"size:16;index" json:"costSource"`
	// PriceRev 是折算时用的单价表版本。改了价能知道哪些行该重算。
	PriceRev int `json:"priceRev,omitempty"`

	// StopReason 是轮次收尾原因，异常率的第一层。只有 end_turn 是说完了。
	StopReason string `gorm:"size:32;index" json:"stopReason"`
	// ErrorCode 是 agent 报回的 JSON-RPC 错误码，0 表示这一轮没报错。
	// 异常率的第二层——**按码分类，文案只用于展示**。
	ErrorCode int    `json:"errorCode,omitempty"`
	ErrorMsg  string `gorm:"size:256" json:"errorMsg,omitempty"`
	// ToolCalls / ToolFailed 是本轮工具调用的终态计数，异常率的第三层。
	ToolCalls  int `json:"toolCalls"`
	ToolFailed int `json:"toolFailed"`

	CreatedAt time.Time `json:"createdAt"`
}
