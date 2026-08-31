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
// 在下次同步时清掉。

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
			{"name": "​", "value": "​", "inline": true},
			{"name": "​", "value": "​", "inline": true},
			{"name": "工作目录", "value": "`" + b.Workdir + "`", "inline": false},
		},
		"footer": map[string]any{"text": "/model 模型 · /effort 深度 · /access 权限 · /init 重绑 · /status 查看"},
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
	s.syncTopic(ctx, token, b)
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
		map[string]any{"topic": trimRunes(topic, 1000)}, nil)
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
			map[string]any{"topic": trimRunes(topicLine(fresh), 1000)}, nil)
		if err != nil {
			slog.Warn("频道主题重写仍失败", "err", err)
		}
	}()
}

// topicLine 是主题摘要的唯一格式。主题是频道侧唯一常驻信息面，工作目录
// 也带上——顶栏截断没关系，点开主题能看全文。
func topicLine(b Binding) string {
	branch := branchLine(b)
	model := b.ModelLabel
	if model == "" {
		model = b.Agent + " · " + b.Model
	}
	effort := b.Effort
	if effort == "" {
		effort = "默认"
	}
	return fmt.Sprintf("acpp 工作区：%s @ %s · %s · 思考深度 %s · 权限 %s · 库 %s · 目录 %s",
		b.Repo, branch, model, effort, accessLabel(b.AccessOrDefault()), dbLine(b), b.Workdir)
}

// branchLine 是「这个频道在哪条分支上干活」的统一口径：工作分支 + 它从
// 哪切出来的。两者都要显示——分支名是自动生成的，只报它看不出对应哪个
// 环境；只报 base 又会让人误以为 agent 直接在 base 上提交。
func branchLine(b Binding) string {
	branch := b.Branch
	if branch == "" {
		branch = "默认分支"
	}
	if b.Base != "" && b.Base != b.Branch {
		return fmt.Sprintf("`%s`（基于 `%s`）", branch, b.Base)
	}
	return "`" + branch + "`"
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
