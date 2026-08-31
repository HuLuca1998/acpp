package discord

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// 频道使用手册：/init 完成后发布到频道并置顶（用户点名要的入门文档——
// 频道零消息原则对它让路：新成员进来第一眼要能看懂怎么用）。重新 /init
// 时旧手册删掉重发，解绑时一并清理。

// guideMD 是手册正文（Discord 方言 markdown：不用表格与深层标题）。
func guideMD(b Binding) string {
	return fmt.Sprintf(`# 🤖 acpp 使用手册

本频道已绑定仓库 **%s** @ %s（%s · 思考深度 %s · 权限 %s）。

## 开始对话
- 在本频道 **@acpp** 说话（@ 出来选用户或角色都行），自动开一个子区，agent 在子区里干活
- 之后直接在子区里聊，不用再 @；手动创建的子区同样有效

## 对话里能做什么
- **发文件**：消息附件直接进对话——图片给模型看，文本嵌入全文，大文件落盘引用
- **查数据库**：数据库工具默认已挂载（按本频道仓库归属的项目过滤）；`+"`/db off`"+` 可卸载
- **要报告**：说「写一份 xx 报告并打开」，报告渲染成整页长图直接出现在子区
- **要图表 / 文件**：agent 生成的图片、图表、文件会直接发进对话（HTML 自动渲染成图）
- **排队与编辑**：回合进行中发的消息标 ⏳ 排队、下一轮自动带上；还标着 ⏳ 的消息可以编辑
- **权限与提问**：agent 请求授权或提问时出卡片——点按钮、填表单，或直接回复编号

## 看懂状态
⏳ 已排队 · ✅ 已进对话；📋 计划卡与 🔧 工具卡实时刷新；回合结束回复末尾有小结（耗时 · 工具 · 改动文件 · token）

## 常用命令
%s`,
		b.Repo, b.Branch, guideModel(b), orDefault(b.Effort, "默认"), b.AccessOrDefault(), commandsLine())
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
