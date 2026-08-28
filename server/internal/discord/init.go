package discord

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
)

// 本文件是 /init 的全流程：绑定表单（仓库下拉 + 模型/深度/权限档）、
// 分支两步选、克隆与绑定落盘。命令注册与事件分发在 interaction.go。

// openInitModal 弹出绑定表单：仓库下拉（gh 的组织仓库清单）+ 自定义输入
// + 模型 + 思考深度。callback 有 3 秒时限——仓库清单限时 2 秒，超时就
// 退化成纯手输，不挡人。
func (s *Service) openInitModal(ctx context.Context, token string, ev interactionEvent) {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var catalog []AgentOption
	if s.deps.Catalog != nil {
		if opts, err := s.deps.Catalog(cctx); err == nil {
			catalog = opts
		}
	}
	models := modelChoices(catalog)
	if len(models) == 0 {
		s.ephemeral(token, ev, "模型清单还没就绪——先在 acpp 设置页完成内置工具探测，再回来 /init。")
		return
	}

	var repos []RepoOption
	if s.deps.Repos != nil {
		if list, err := s.deps.Repos(cctx); err != nil {
			slog.Warn("取仓库清单失败，/init 退化为手输", "err", err)
		} else {
			repos = list
		}
	}

	current, _ := s.store.config().binding(ev.ChannelID)
	customInput := map[string]any{
		"type": 4, "custom_id": "repo_custom", "style": 1, "required": len(repos) == 0,
		"placeholder": "BDBGAME2024/pp-game 或完整 clone 地址",
	}
	if current.Repo != "" {
		customInput["value"] = current.Repo
	}

	var comps []map[string]any
	if len(repos) > 0 {
		comps = append(comps, map[string]any{
			"type": 18, "label": "仓库", "description": "组织仓库清单（gh）",
			"component": selectComponent("repo_pick", repoChoices(repos), false),
		})
		comps = append(comps, map[string]any{
			"type": 18, "label": "自定义仓库", "description": "下拉里没有时填这里，以填写的为准",
			"component": customInput,
		})
	} else {
		comps = append(comps, map[string]any{
			"type": 18, "label": "Git 仓库", "description": "owner/repo 简写按 GitHub 解析",
			"component": customInput,
		})
	}
	comps = append(comps,
		map[string]any{"type": 18, "label": "模型", "component": selectComponent("model", models, true)},
		map[string]any{"type": 18, "label": "思考深度", "component": selectComponent("effort", effortChoices(), true)},
		map[string]any{"type": 18, "label": "安全权限", "component": selectComponent("access", accessChoices(), true)},
	)

	data := map[string]any{
		"custom_id":  "init",
		"title":      "绑定频道工作区",
		"components": comps,
	}
	if err := interactionCallback(token, ev.ID, ev.Token, 9, data); err != nil {
		slog.Error("弹 /init 表单失败", "err", err)
	}
}

// modelChoices 把 catalog 拍平成「agent · 模型」的选择项；value 编码为
// `agent|modelID`，提交时拆回。选项超过平台上限（25）截断。
func modelChoices(catalog []AgentOption) []choice {
	var out []choice
	for _, a := range catalog {
		for _, m := range a.Models {
			out = append(out, choice{
				Label: a.Agent + " · " + m.Label,
				Value: a.Agent + "|" + m.ID,
			})
		}
	}
	if len(out) > 25 {
		slog.Warn("模型选项超过 25，已截断", "total", len(out))
		out = out[:25]
	}
	return out
}

// repoChoices 把远端仓库清单变成下拉项（gh 已按更新时间排序，截前 25）。
func repoChoices(repos []RepoOption) []choice {
	var out []choice
	for _, r := range repos {
		out = append(out, choice{Label: r.Name, Value: r.Name})
	}
	if len(out) > 25 {
		out = out[:25]
	}
	return out
}

// effortChoices 是统一思考深度五档 + 默认（值 default，落盘转空串）。
func effortChoices() []choice {
	return []choice{
		{Label: "默认", Value: "default", Description: "跟随 agent 自己的默认档"},
		{Label: "low", Value: "low"},
		{Label: "medium", Value: "medium"},
		{Label: "high", Value: "high"},
		{Label: "xhigh", Value: "xhigh"},
		{Label: "max", Value: "max"},
	}
}

// accessLabel 是权限档的展示名（认不出的档位返回空串，兼作校验）。
func accessLabel(v string) string {
	for _, c := range accessChoices() {
		if c.Value == v {
			return c.Label
		}
	}
	return ""
}

// accessChoices 是统一权限三档（对齐 acp.AccessLevel 词汇）。
func accessChoices() []choice {
	return []choice{
		{Label: "自动编辑", Value: "auto-edit", Description: "自动接受编辑，危险命令仍拦"},
		{Label: "完全放开", Value: "full", Description: "跳过一切确认，适合无人值守"},
		{Label: "安全", Value: "safe", Description: "写操作逐项确认（要有人批）"},
	}
}

// selectComponent 选组件：≤10 项用 RadioGroup（一眼全见），更多用
// String Select（实测结论，速查 §8）。
func selectComponent(customID string, choices []choice, required bool) map[string]any {
	kind := 21
	if len(choices) > 10 {
		kind = 3
	}
	opts := make([]map[string]any, 0, len(choices))
	for _, c := range choices {
		opt := map[string]any{"label": trimRunes(c.Label, 90), "value": trimRunes(c.Value, 90)}
		if c.Description != "" {
			opt["description"] = trimRunes(c.Description, 90)
		}
		opts = append(opts, opt)
	}
	out := map[string]any{"type": kind, "custom_id": customID, "options": opts, "required": required}
	if kind == 3 {
		out["placeholder"] = "选一个…"
	}
	return out
}

// submitInit 收表单：立即回 deferred（后面要现查分支、可能还要克隆几分钟），
// 后台探分支——多于一个分支就把回执卡编辑成分支下拉，否则直接开工。
func (s *Service) submitInit(ctx context.Context, token string, ev interactionEvent) {
	answers := parseModalSubmit(ev.Data.Components)
	repoIn := strings.TrimSpace(firstAnswer(answers, "repo_custom"))
	if repoIn == "" {
		repoIn = firstAnswer(answers, "repo_pick")
	}
	agent, modelID, ok := strings.Cut(firstAnswer(answers, "model"), "|")
	if !ok || repoIn == "" {
		s.ephemeral(token, ev, "表单不完整（仓库没填/没选），重新 /init 一次。")
		return
	}
	name, cloneURL, err := resolveRepo(repoIn)
	if err != nil {
		s.ephemeral(token, ev, err.Error())
		return
	}
	effort := firstAnswer(answers, "effort")
	if effort == "default" {
		effort = ""
	}

	// type 5 + ephemeral = 过程只有发起者可见。频道里永远只有一张身份卡，
	// 分支选择、进行中、失败原因这些过程态不进频道时间线。
	if err := interactionCallback(token, ev.ID, ev.Token, 5, map[string]any{"flags": 1 << 6}); err != nil {
		slog.Error("/init deferred 回执失败", "err", err)
		return
	}

	go s.offerBranches(ctx, token, ev, initInput{
		repo: name, cloneURL: cloneURL,
		agent: agent, modelID: modelID, effort: effort, access: firstAnswer(answers, "access"),
	})
}

// offerBranches 现查远端分支：只有一个（或查不动）就直接按默认分支开工，
// 多个则把回执卡编辑成分支下拉，等下一次点击。
func (s *Service) offerBranches(ctx context.Context, token string, ev interactionEvent, in initInput) {
	appID := s.appID()
	lctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defaultBranch, branches, err := lsRemoteBranches(lctx, in.cloneURL)
	cancel()
	in.defaultBranch = defaultBranch
	if err != nil || len(branches) <= 1 {
		if err != nil {
			slog.Warn("查分支失败，按默认分支继续", "repo", in.repo, "err", err)
		}
		s.finishInit(ctx, token, ev, in)
		return
	}

	id, perr := randomID()
	if perr != nil {
		s.finishInit(ctx, token, ev, in)
		return
	}
	s.mu.Lock()
	for k, p := range s.pending {
		// interaction token 只活 15 分钟，过期的中途状态一起清。
		if time.Since(p.created) > 14*time.Minute {
			delete(s.pending, k)
		}
	}
	s.pending[id] = pendingInit{in: in, ev: ev, created: time.Now()}
	s.mu.Unlock()

	var opts []choice
	opts = append(opts, choice{Label: defaultBranch + "（默认）", Value: defaultBranch})
	for _, b := range branches {
		if b == defaultBranch {
			continue
		}
		opts = append(opts, choice{Label: b, Value: b})
		if len(opts) == 25 {
			break
		}
	}
	sel := selectComponent("br:"+id, opts, false)
	sel["type"] = 3 // 消息上的下拉只有 String Select，radio 是 modal 的东西
	sel["placeholder"] = "选择分支…"
	delete(sel, "required")

	s.editOriginal(token, appID, ev.Token, map[string]any{
		"embeds": []map[string]any{{
			"title":       "选择分支",
			"description": fmt.Sprintf("**%s** 有 %d 个分支。15 分钟内有效，过期请重新 /init。", in.repo, len(branches)),
			"color":       colorBlurbe,
		}},
		"components":       []map[string]any{{"type": 1, "components": []map[string]any{sel}}},
		"allowed_mentions": noMentions(),
	})
}

// branchPicked 收分支下拉的选择：卡片原地改成进行中，然后照常收尾。
func (s *Service) branchPicked(ctx context.Context, token string, ev interactionEvent) {
	id := strings.TrimPrefix(ev.Data.CustomID, "br:")
	s.mu.Lock()
	p, ok := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if !ok || len(ev.Data.Values) == 0 {
		s.ephemeral(token, ev, "这张卡过期了（或已处理过），重新 /init 一次。")
		return
	}
	in := p.in
	if v := ev.Data.Values[0]; v != in.defaultBranch {
		in.branch = v
	}

	label := in.branch
	if label == "" {
		label = in.defaultBranch + "（默认）"
	}
	// type 7 = 原地改卡：把下拉摘掉、亮出进行中，接管这张卡。
	err := interactionCallback(token, ev.ID, ev.Token, 7, map[string]any{
		"embeds": []map[string]any{{
			"title":       "⏳ 正在准备工作区…",
			"description": fmt.Sprintf("**%s** · 分支 **%s**", in.repo, label),
			"color":       colorBlurbe,
		}},
		"components":       []map[string]any{},
		"allowed_mentions": noMentions(),
	})
	if err != nil {
		slog.Error("分支选择改卡失败", "err", err)
	}
	// 收尾仍编辑最初 /init 的回执（同一条消息，用建卡那次的 token）。
	go s.finishInit(ctx, token, p.ev, in)
}

// finishInit 是 /init 的慢半段：克隆（或复用）、落绑定、把频道主题刷成
// 最新摘要（频道侧唯一常驻信息面），ephemeral 回执给一句结论后阅后即焚。
func (s *Service) finishInit(ctx context.Context, token string, ev interactionEvent, in initInput) {
	appID := s.appID()
	workdir := filepath.Join(s.effectiveWorkRoot(s.store.config()), filepath.FromSlash(workdirName(in.repo, in.branch)))

	reused, err := ensureWorkdir(ctx, in.cloneURL, in.branch, workdir)
	if err != nil {
		s.editOriginal(token, appID, ev.Token, map[string]any{
			"embeds": []map[string]any{{
				"title":       "❌ 工作区创建失败",
				"description": fmt.Sprintf("**%s**\n```\n%s\n```", in.repo, trimRunes(err.Error(), 900)),
				"color":       colorRed,
			}},
			"components":       []map[string]any{},
			"allowed_mentions": noMentions(),
		})
		return
	}

	// 频道名是展示锦上添花，查不到不挡流程。
	channelName := ""
	var ch struct {
		Name string `json:"name"`
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if err := botREST(cctx, token, "GET", "/channels/"+ev.ChannelID, nil, &ch); err == nil {
		channelName = ch.Name
	}
	cancel()

	// CardMessageID 只为清掉历史遗留的置顶卡（卡已退役），继承后交给
	// syncChannelCard 收尾。
	old, _ := s.store.config().binding(ev.ChannelID)
	now := time.Now()
	binding := Binding{
		ChannelID: ev.ChannelID, ChannelName: channelName, GuildID: ev.GuildID,
		Repo: in.repo, CloneURL: in.cloneURL, Branch: in.branch, Workdir: workdir,
		Agent: in.agent, Model: in.modelID, ModelLabel: s.modelLabel(ctx, in.agent, in.modelID),
		Effort: in.effort, Access: in.access,
		CardMessageID: old.CardMessageID, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := s.store.update(func(c *Config) { c.upsertBinding(binding) }); err != nil {
		slog.Error("绑定落盘失败", "channel", ev.ChannelID, "err", err)
		s.editOriginal(token, appID, ev.Token, map[string]any{
			"embeds": []map[string]any{{
				"title": "❌ 绑定保存失败", "description": trimRunes(err.Error(), 900), "color": colorRed,
			}},
			"components":       []map[string]any{},
			"allowed_mentions": noMentions(),
		})
		return
	}

	source := "已克隆"
	if reused {
		source = "复用已有克隆"
	}
	s.syncChannelCard(ctx, token, binding)
	s.editOriginal(token, appID, ev.Token, map[string]any{
		"content":          fmt.Sprintf("✅ **%s** 工作区已就绪（%s）。绑定详情看频道主题，或随时 /status。", in.repo, source),
		"embeds":           []map[string]any{},
		"components":       []map[string]any{},
		"allowed_mentions": noMentions(),
	})
	s.deleteOriginalLater(token, appID, ev.Token)
}

// choice 是一条选择项（radio / string select 通吃）。
type choice struct {
	Label       string
	Value       string
	Description string
}

// pendingInit 是「表单已交、等选分支」的中途状态。
type pendingInit struct {
	in      initInput
	ev      interactionEvent
	created time.Time
}

type initInput struct {
	repo, cloneURL, agent, modelID, effort, access string
	// branch 空 = 默认分支；defaultBranch 用于把「选了默认」归一成空。
	branch, defaultBranch string
}
