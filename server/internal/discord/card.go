package discord

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 本文件是频道侧展示面。频道里不留任何 bot 消息（用户拍板）：常驻信息
// 面只有**频道主题**一行摘要；详情卡（bindingEmbed）只在 /status 时以
// ephemeral 出现，看完自动消失。早期版本的置顶身份卡已退役，遗留的卡
// 在下次同步时清掉。唯一的例外是置顶的**频道使用手册**（本文件末段）
// ——新成员进来第一眼要能看懂怎么用，零消息原则对它让路。

// bindingEmbed 是绑定详情卡（/status 专用，ephemeral）。
func bindingEmbed(b Binding, source string) map[string]any {
	branch := branchLine(b)
	effort := b.Effort
	if effort == "" {
		effort = "默认"
	}
	model := b.ModelLabel
	if model == "" {
		model = b.Agent + " · " + b.Model
	}
	desc := ""
	if source != "" {
		desc = source + "，之后这个频道的工作目录就是它。"
	}
	return map[string]any{
		"title":       "✅ 频道工作区",
		"description": desc,
		"color":       colorGreen,
		"fields": []map[string]any{
			{"name": "仓库", "value": "`" + b.Repo + "`", "inline": true},
			{"name": "工作分支", "value": branch, "inline": true},
			{"name": "​", "value": "​", "inline": true},
			{"name": "模型", "value": model, "inline": true},
			{"name": "思考深度", "value": effort, "inline": true},
			{"name": "安全权限", "value": accessLabel(b.AccessOrDefault()), "inline": true},
			{"name": "数据库", "value": dbLine(b), "inline": true},
			{"name": "服务器", "value": serverLine(b), "inline": true},
			{"name": "​", "value": "​", "inline": true},
			{"name": "工作目录", "value": "`" + b.Workdir + "`", "inline": false},
		},
		"footer": map[string]any{"text": "/model 模型 · /effort 深度 · /access 权限 · /db 换库 · /server 换机器 · /init 重绑"},
	}
}

// cleanupChannelCard 解绑后的频道侧收尾：删遗留卡（如有）、清主题。
// 都是尽力而为——解绑后频道不该留任何绑定痕迹。
func (s *Service) cleanupChannelCard(ctx context.Context, token string, b Binding) {
	s.retireCard(ctx, token, b.ChannelID, b.CardMessageID)
	if b.GuideMessageID != "" {
		gctx, gcancel := context.WithTimeout(ctx, 5*time.Second)
		_ = botREST(gctx, token, "DELETE", "/channels/"+b.ChannelID+"/messages/"+b.GuideMessageID, nil, nil)
		gcancel()
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := botREST(cctx, token, "PATCH", "/channels/"+b.ChannelID,
		map[string]any{"topic": ""}, nil); err != nil {
		slog.Warn("清频道主题失败（可能撞限速）", "err", err)
	}
}

// syncChannelCard 让频道侧展示跟上配置：主题刷成最新摘要；早期版本的
// 置顶卡如果还挂着，趁这次同步退役掉。
func (s *Service) syncChannelCard(ctx context.Context, token string, b Binding) {
	if b.CardMessageID != "" {
		s.retireCard(ctx, token, b.ChannelID, b.CardMessageID)
		b.CardMessageID = ""
		if _, err := s.store.update(func(c *Config) {
			for i := range c.Bindings {
				if c.Bindings[i].ChannelID == b.ChannelID {
					c.Bindings[i].CardMessageID = ""
				}
			}
		}); err != nil {
			slog.Warn("清身份卡 id 失败", "err", err)
		}
	}
	s.syncGuide(ctx, token, b)
	s.syncTopic(ctx, token, b)
}

// syncGuide 把置顶手册编辑成最新内容（消息 PATCH，不重发——重发会把置顶
// 和阅读位置都打乱）。
//
// 手册里带着绑定信息（在哪条分支上干活、锁定了哪个库），绑定一变它就过期；
// 手册文案本身改了也一样——不跟着更新的话，频道里就一直躺着上一版。
func (s *Service) syncGuide(ctx context.Context, token string, b Binding) {
	if b.GuideMessageID == "" {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := botREST(cctx, token, "PATCH",
		fmt.Sprintf("/channels/%s/messages/%s", b.ChannelID, b.GuideMessageID),
		map[string]any{"content": guideMD(b), "allowed_mentions": noMentions()}, nil)
	if err != nil {
		slog.Warn("更新频道手册失败", "channel", b.ChannelID, "err", err)
	}
}

// retireCard 删掉一张历史遗留的置顶身份卡（删除消息连带解除置顶）。
func (s *Service) retireCard(ctx context.Context, token, channelID, messageID string) {
	if messageID == "" {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := botREST(cctx, token, "DELETE",
		fmt.Sprintf("/channels/%s/messages/%s", channelID, messageID), nil, nil); err != nil {
		slog.Warn("删遗留身份卡失败", "err", err)
	}
}

// syncTopic 把摘要写进频道主题（频道侧唯一常驻信息面）。平台对改主题
// 限速很狠（每频道 10 分钟 2 次，实测 429 的 retry_after 能到 5 分钟），
// 撞了就按它说的时间挂一个延迟重写——重写时取最新绑定状态，中间的连续
// 变更自动收敛成一次。每频道最多挂一个重试，绑定没了就作罢。
func (s *Service) syncTopic(ctx context.Context, token string, b Binding) {
	topic := topicLine(b)
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := botREST(cctx, token, "PATCH", "/channels/"+b.ChannelID,
		map[string]any{"topic": trimRunes(topic, topicLimit)}, nil)
	if err == nil {
		return
	}
	delay, ok := retryAfter(err)
	if !ok {
		slog.Warn("写频道主题失败", "err", err)
		return
	}
	s.mu.Lock()
	already := s.topicRetry[b.ChannelID]
	if !already {
		s.topicRetry[b.ChannelID] = true
	}
	s.mu.Unlock()
	if already {
		return
	}
	slog.Info("频道主题撞限速，稍后重写", "channel", b.ChannelID, "delay", delay)
	go func() {
		select {
		case <-time.After(delay + time.Second):
		case <-ctx.Done():
		}
		s.mu.Lock()
		delete(s.topicRetry, b.ChannelID)
		s.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		fresh, ok := s.store.config().binding(b.ChannelID)
		if !ok {
			return
		}
		rctx, rcancel := context.WithTimeout(ctx, 10*time.Second)
		defer rcancel()
		err := botREST(rctx, token, "PATCH", "/channels/"+fresh.ChannelID,
			map[string]any{"topic": trimRunes(topicLine(fresh), topicLimit)}, nil)
		if err != nil {
			slog.Warn("频道主题重写仍失败", "err", err)
		}
	}()
}

// topicLimit 是写进频道主题的字符上限。平台的硬上限是 1024，这里留一截
// 余量：手册那段是固定的，能变长的只有仓库名、分支名和工作目录，超长时
// 宁可把目录掐短（shortPath）也不能让末尾的手册被平台截掉。
const topicLimit = 1000

// shortPath 把过长的路径掐成 `…/尾部`：工作树名比一长串前缀有用得多。
func shortPath(p string, max int) string {
	r := []rune(p)
	if len(r) <= max {
		return p
	}
	return "…" + string(r[len(r)-max+1:])
}

// topicLine 渲染频道主题：频道侧常驻的**绑定信息面**，一行一条。用法说明
// 不放这里——那是置顶手册的活，两份重复只会漂移。
//
// 三条硬约束：
//
//   - 主题上限 1024 字符，这里压在 topicLimit 以内；能变长的只有仓库名、
//     分支名和工作目录，各自有配额，超长掐目录（TestTopicLineFits 盯着）。
//   - **主题不渲染 markdown**——反引号、星号都会原样显示（真机实测），
//     排版只用换行与空行。
//   - 开头必须留一个换行：频道欢迎页把主题直接接在「这是 #xxx 频道的
//     起点。」后面，不空一行首行就跟那句话挤在一起（真机实测）。
func topicLine(b Binding) string {
	model := b.ModelLabel
	if model == "" {
		model = b.Agent + " · " + b.Model
	}
	var w strings.Builder
	w.WriteString("\n项目\n")
	fmt.Fprintf(&w, "仓库：%s\n", trimRunes(b.Repo, 60))
	fmt.Fprintf(&w, "分支：%s\n", trimRunes(branchLine(b), 80))
	fmt.Fprintf(&w, "目录：%s\n", shortPath(b.Workdir, 120))
	w.WriteString("提交都落在上面这条分支上，base 分支不受影响\n\n")

	fmt.Fprintf(&w, "模型：%s\n", trimRunes(model, 50))
	fmt.Fprintf(&w, "思考深度：%s\n", trimRunes(orDefault(b.Effort, "默认"), 20))
	fmt.Fprintf(&w, "权限：%s\n", accessLabel(b.AccessOrDefault()))
	fmt.Fprintf(&w, "数据库：%s", trimRunes(dbTopicLine(b), 70))
	// 服务器另起一行，不跟数据库挤在一起——那一行本来就带着「只有这一个，
	// 别的环境查不到」的补语，后面再缀一段就得横着读老半天。
	//
	// 只在锁定时才写：没锁定是常态（服务器不做项目隔离），每个频道都挂
	// 一句「不锁定」只是噪声。
	if b.ServerID != 0 {
		fmt.Fprintf(&w, "\n服务器：%s — 只有这一台，别的机器看不到", trimRunes(serverLine(b), 40))
	}
	return w.String()
}

// serverLine 是「这个频道能看哪台机器」的统一口径（主题、/status 共用）。
// 名字是落盘的展示快照，服务器被删了也还说得清原本绑的是谁。
func serverLine(b Binding) string {
	switch {
	case b.ServerID == 0:
		return "不锁定"
	case b.ServerName != "":
		return b.ServerName
	default:
		return fmt.Sprintf("#%d", b.ServerID)
	}
}

// dbTopicLine 是主题里的数据库那行：锁定了就把「别的环境查不到」说明白，
// 这是三个环境频道之间唯一的实质差别。
func dbTopicLine(b Binding) string {
	if b.DataSourceID == 0 {
		return "不锁定（本项目的数据源都可见）"
	}
	return dbLine(b) + " — 只有这一个，别的环境查不到"
}

// branchLine 是「这个频道在哪条分支上干活」的统一口径：工作分支 + 它从
// 哪切出来的。两者都要显示——分支名是自动生成的，只报它看不出对应哪个
// 环境；只报 base 又会让人误以为 agent 直接在 base 上提交。
//
// 不带反引号：主题不渲染 markdown，写了就是多两个字符的噪声；/status 的
// 卡片里要代码样式的话由那边自己包。
func branchLine(b Binding) string {
	branch := b.Branch
	if branch == "" {
		branch = "默认分支"
	}
	if b.Base != "" && b.Base != b.Branch {
		return fmt.Sprintf("%s（基于 %s）", branch, b.Base)
	}
	return branch
}

// dbLine 是「这个频道能查哪个库」的统一口径（主题、/status、手册、/mcps
// 共用一句话）。ref 是落盘的展示快照，连接被删了也还说得清原本绑的是谁。
func dbLine(b Binding) string {
	switch {
	case b.DataSourceID == 0:
		return "不锁定"
	case b.DataSourceRef != "":
		return b.DataSourceRef
	default:
		return fmt.Sprintf("#%d", b.DataSourceID)
	}
}

// retryAfter 从 429 错误文本里抠 retry_after 秒数（botREST 的错误带响应体）。
func retryAfter(err error) (time.Duration, bool) {
	msg := err.Error()
	if !strings.Contains(msg, "429") {
		return 0, false
	}
	m := retryAfterRe.FindStringSubmatch(msg)
	if m == nil {
		// 429 但没解出时长：给个保守值，总比放弃强。
		return 5 * time.Minute, true
	}
	secs, perr := strconv.ParseFloat(m[1], 64)
	if perr != nil || secs <= 0 || secs > 3600 {
		return 5 * time.Minute, true
	}
	return time.Duration(secs * float64(time.Second)), true
}

var retryAfterRe = regexp.MustCompile(`"retry_after":\s*([0-9.]+)`)

// editOriginal 编辑 deferred 回执（webhook 路径，15 分钟时效）。
func (s *Service) editOriginal(token, appID, interactionToken string, body map[string]any) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := botREST(ctx, token, "PATCH",
		fmt.Sprintf("/webhooks/%s/%s/messages/@original", appID, interactionToken), body, nil)
	if err != nil {
		slog.Error("回写 /init 结果失败", "err", err)
	}
}

// 频道使用手册：/init 完成后发布到频道并置顶（用户点名要的入门文档——
// 频道零消息原则对它让路：新成员进来第一眼要能看懂怎么用）。重新 /init
// 时旧手册删掉重发，解绑时一并清理。

// guideMD 是手册正文（Discord 方言 markdown：不用表格与深层标题）。
func guideMD(b Binding) string {
	return fmt.Sprintf(`# 🤖 acpp 使用手册

本频道已绑定仓库 **%s**，在 %s 上干活（%s · 思考深度 %s · 权限 %s）。
-# 提交都落在这条自己的分支上，base 分支不受影响。

## 开始对话
- 在本频道 **@acpp** 说话（@ 出来选用户或角色都行），自动开一个子区，agent 在子区里干活
- 之后直接在子区里聊，不用再 @；手动创建的子区同样有效

## 对话里能做什么
- **发文件**：消息附件直接进对话——图片给模型看，文本嵌入全文，大文件落盘引用
- **查数据库**：数据库工具默认已挂载（%s）；`+"`/db off`"+` 可卸载
- **要报告**：说「写一份 xx 报告并打开」，子区里出一张卡，点「打开报告」就是排好版的页面
- **要文件**：说「把 xx 发上来」，文件作为附件上传（可下载、转发）；说「发文件别发链接」就一定给原件
- **要链接 / 撤链接**：报告与 HTML 默认发**外链**（7 天失效，拿到链接的人都能打开）；
  说「看完了 / 删掉链接」随时撤销，问「还有哪些链接」可以列出来
- **排队与编辑**：回合进行中发的消息标 ⏳ 排队、下一轮自动带上；还标着 ⏳ 的消息可以编辑
- **权限与提问**：agent 请求授权或提问时出卡片——点按钮、填表单，或直接回复编号

## 看代码改动
- `+"`/git`"+` 列出本频道工作树的分支、与远端的差距，以及改了 / 新增 / 删除了哪些文件

## 看懂状态
⏳ 已排队 · ✅ 已进对话；📋 计划卡与 🔧 工具卡实时刷新；回合结束回复末尾有小结（耗时 · 工具 · 改动文件 · token）

## 常用命令
在输入框打 `+"`/`"+` 就能看到全部命令，每条都带说明。`,
		b.Repo, branchLine(b), guideModel(b), orDefault(b.Effort, "默认"), b.AccessOrDefault(),
		guideDBScope(b))
}

// guideDBScope 说明这个频道的数据库能看到什么：锁定了就点名那一条连接
// （一个项目的几个环境频道各绑各的库，这句话是频道之间唯一的区别），
// 没锁定就是老口径的项目过滤。
func guideDBScope(b Binding) string {
	if b.DataSourceID != 0 {
		return "本频道锁定 " + dbLine(b) + "，别的环境查不到"
	}
	return "按本频道仓库归属的项目过滤"
}

// guideModel 是手册头部的模型描述。ModelLabel 来自 /init 表单时自带
// agent 前缀（「claude · Default」），别再拼一次 agent（会重复）。
func guideModel(b Binding) string {
	if b.ModelLabel != "" {
		return b.ModelLabel
	}
	return b.Agent + " · " + orDefault(b.Model, "默认模型")
}

// publishGuide 发布（或更新）频道手册：删旧发新、置顶、落盘消息 id。
func (s *Service) publishGuide(ctx context.Context, token string, b Binding) {
	if b.GuideMessageID != "" {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_ = botREST(cctx, token, "DELETE", "/channels/"+b.ChannelID+"/messages/"+b.GuideMessageID, nil, nil)
		cancel()
	}
	var msg struct {
		ID string `json:"id"`
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err := botREST(cctx, token, "POST", "/channels/"+b.ChannelID+"/messages", map[string]any{
		"content":          guideMD(b),
		"allowed_mentions": noMentions(),
	}, &msg)
	cancel()
	if err != nil || msg.ID == "" {
		slog.Warn("发布频道手册失败", "channel", b.ChannelID, "err", err)
		return
	}
	// 置顶失败不算失败——手册已经在频道里了。
	cctx, cancel = context.WithTimeout(ctx, 5*time.Second)
	if err := botREST(cctx, token, "PUT", "/channels/"+b.ChannelID+"/pins/"+msg.ID, nil, nil); err != nil {
		slog.Warn("手册置顶失败", "err", err)
	}
	cancel()
	if _, err := s.store.update(func(c *Config) {
		if cur, ok := c.binding(b.ChannelID); ok {
			cur.GuideMessageID = msg.ID
			c.upsertBinding(cur)
		}
	}); err != nil {
		slog.Warn("手册消息 id 落盘失败", "err", err)
	}
}
