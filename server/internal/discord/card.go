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

// 本文件是频道侧展示面：工作区身份卡（置顶）、频道主题摘要，以及它们的
// 同步与收尾。卡片形状只有 bindingEmbed 一处，/init、/status、配置变更
// 与网页端编辑共用。

// bindingEmbed 是频道工作区的身份卡：/init 的结果卡、置顶卡与 /status
// 共用一个形状，配置变化时原地刷新。
func bindingEmbed(b Binding, source string) map[string]any {
	branch := b.Branch
	if branch == "" {
		branch = "默认"
	}
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
			{"name": "分支", "value": "`" + branch + "`", "inline": true},
			{"name": "​", "value": "​", "inline": true},
			{"name": "模型", "value": model, "inline": true},
			{"name": "思考深度", "value": effort, "inline": true},
			{"name": "​", "value": "​", "inline": true},
			{"name": "工作目录", "value": "`" + b.Workdir + "`", "inline": false},
		},
		"footer": map[string]any{"text": "/model 换模型 · /effort 换深度 · /init 重绑 · /status 查看"},
	}
}

// cleanupChannelCard 解绑后的频道侧收尾：摘置顶、删身份卡、清主题。
// 都是尽力而为——解绑后频道不该留任何绑定痕迹（单卡原则的另一半）。
func (s *Service) cleanupChannelCard(ctx context.Context, token string, b Binding) {
	if b.CardMessageID != "" {
		s.unpinMessage(ctx, token, b.ChannelID, b.CardMessageID)
		dctx, dcancel := context.WithTimeout(ctx, 10*time.Second)
		if err := botREST(dctx, token, "DELETE",
			fmt.Sprintf("/channels/%s/messages/%s", b.ChannelID, b.CardMessageID), nil, nil); err != nil {
			slog.Warn("删身份卡失败", "err", err)
		}
		dcancel()
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := botREST(cctx, token, "PATCH", "/channels/"+b.ChannelID,
		map[string]any{"topic": ""}, nil); err != nil {
		slog.Warn("清频道主题失败（可能撞限速）", "err", err)
	}
}

// syncChannelCard 让频道里的展示跟上配置：置顶卡原地刷新（被删了就补发
// 一张再置顶），频道主题写一行摘要。
func (s *Service) syncChannelCard(ctx context.Context, token string, b Binding) {
	body := map[string]any{
		"embeds":           []map[string]any{bindingEmbed(b, "")},
		"allowed_mentions": noMentions(),
	}
	if b.CardMessageID != "" {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := botREST(cctx, token, "PATCH",
			fmt.Sprintf("/channels/%s/messages/%s", b.ChannelID, b.CardMessageID), body, nil)
		cancel()
		if err != nil {
			slog.Warn("置顶卡刷新失败，补发新卡", "err", err)
			b.CardMessageID = ""
		}
	}
	if b.CardMessageID == "" {
		var msg struct {
			ID string `json:"id"`
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := botREST(cctx, token, "POST",
			fmt.Sprintf("/channels/%s/messages", b.ChannelID), body, &msg)
		cancel()
		if err == nil && msg.ID != "" {
			s.pinMessage(ctx, token, b.ChannelID, msg.ID)
			s.deletePinNotice(ctx, token, b.ChannelID)
			b.CardMessageID = msg.ID
			if _, err := s.store.update(func(c *Config) { c.upsertBinding(b) }); err != nil {
				slog.Warn("身份卡 id 落盘失败", "err", err)
			}
		}
	}
	s.syncTopic(ctx, token, b)
}

// deletePinNotice 把置顶动作产生的系统消息（type 6）从时间线里清掉——
// 频道只留身份卡本身。尽力而为，找不到就算了。
func (s *Service) deletePinNotice(ctx context.Context, token, channelID string) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var msgs []struct {
		ID   string `json:"id"`
		Type int    `json:"type"`
	}
	if err := botREST(cctx, token, "GET",
		fmt.Sprintf("/channels/%s/messages?limit=5", channelID), nil, &msgs); err != nil {
		return
	}
	for _, m := range msgs {
		if m.Type == 6 {
			if err := botREST(cctx, token, "DELETE",
				fmt.Sprintf("/channels/%s/messages/%s", channelID, m.ID), nil, nil); err != nil {
				slog.Warn("清置顶系统消息失败", "err", err)
			}
			return
		}
	}
}

// syncTopic 把一行摘要写进频道主题（顶部常驻）。平台对改主题限速很狠
// （每频道 10 分钟 2 次，实测 429 的 retry_after 能到 5 分钟），撞了就按
// 它说的时间挂一个延迟重写——重写时取最新绑定状态，中间的连续变更自动
// 收敛成一次。每频道最多挂一个重试，绑定没了就作罢。
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

// topicLine 是主题摘要的唯一格式。
func topicLine(b Binding) string {
	branch := b.Branch
	if branch == "" {
		branch = "默认分支"
	}
	model := b.ModelLabel
	if model == "" {
		model = b.Agent + " · " + b.Model
	}
	effort := b.Effort
	if effort == "" {
		effort = "默认"
	}
	return fmt.Sprintf("acpp 工作区：%s @ %s · %s · 思考深度 %s", b.Repo, branch, model, effort)
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

// pinMessage 置顶消息：先走新路由，老路由兜底（平台 2025 年换过端点）。
func (s *Service) pinMessage(ctx context.Context, token, channelID, messageID string) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := botREST(cctx, token, "PUT",
		fmt.Sprintf("/channels/%s/messages/pins/%s", channelID, messageID), nil, nil)
	if err != nil {
		if err2 := botREST(cctx, token, "PUT",
			fmt.Sprintf("/channels/%s/pins/%s", channelID, messageID), nil, nil); err2 != nil {
			slog.Warn("置顶身份卡失败", "err", err, "legacyErr", err2)
		}
	}
}

func (s *Service) unpinMessage(ctx context.Context, token, channelID, messageID string) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := botREST(cctx, token, "DELETE",
		fmt.Sprintf("/channels/%s/messages/pins/%s", channelID, messageID), nil, nil)
	if err != nil {
		if err2 := botREST(cctx, token, "DELETE",
			fmt.Sprintf("/channels/%s/pins/%s", channelID, messageID), nil, nil); err2 != nil {
			slog.Warn("摘旧身份卡失败", "err", err, "legacyErr", err2)
		}
	}
}

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
