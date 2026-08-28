package discord

import (
	"context"
	"fmt"
	"log/slog"
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

// cleanupChannelCard 解绑后的频道侧收尾：摘置顶卡、清主题。都是尽力而为。
func (s *Service) cleanupChannelCard(ctx context.Context, token string, b Binding) {
	if b.CardMessageID != "" {
		s.unpinMessage(ctx, token, b.ChannelID, b.CardMessageID)
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
			b.CardMessageID = msg.ID
			if _, err := s.store.update(func(c *Config) { c.upsertBinding(b) }); err != nil {
				slog.Warn("身份卡 id 落盘失败", "err", err)
			}
		}
	}
	s.syncTopic(ctx, token, b)
}

// syncTopic 把一行摘要写进频道主题（顶部常驻）。平台对改主题限速很狠
// （每频道 10 分钟 2 次），失败只记日志——置顶卡才是权威展示。
func (s *Service) syncTopic(ctx context.Context, token string, b Binding) {
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
	topic := fmt.Sprintf("acpp 工作区：%s @ %s · %s · 思考深度 %s", b.Repo, branch, model, effort)
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := botREST(cctx, token, "PATCH", "/channels/"+b.ChannelID,
		map[string]any{"topic": trimRunes(topic, 1000)}, nil)
	if err != nil {
		slog.Warn("写频道主题失败（可能撞限速）", "err", err)
	}
}

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
