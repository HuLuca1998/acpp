package discord

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
)

// Discord 品牌色（结果卡用）。
const (
	colorGreen  = 0x57F287
	colorRed    = 0xED4245
	colorBlurbe = 0x5865F2
)

// registerCommands 注册 guild 级斜杠命令（即时生效；global 有传播延迟）。
// PUT 语义是全量覆盖，幂等，每次连接对每个 guild 执行一遍。/model 的
// 选项 choices 来自 catalog 快照——模型清单变了要重连（或重启）才刷新，
// 换来的是原生下拉体验（不用弹表单）。
func (s *Service) registerCommands(ctx context.Context, token, appID, guildID string) {
	var modelChoicesJSON []map[string]any
	if s.deps.Catalog != nil {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if catalog, err := s.deps.Catalog(cctx); err == nil {
			for _, c := range modelChoices(catalog) {
				modelChoicesJSON = append(modelChoicesJSON, map[string]any{
					"name": trimRunes(c.Label, 90), "value": c.Value,
				})
			}
		}
		cancel()
	}
	modelOption := map[string]any{
		"type": 3, "name": "model", "description": "要切换到的模型", "required": true,
	}
	if len(modelChoicesJSON) > 0 {
		modelOption["choices"] = modelChoicesJSON
	}
	var effortChoicesJSON []map[string]any
	for _, c := range effortChoices() {
		effortChoicesJSON = append(effortChoicesJSON, map[string]any{
			"name": c.Label, "value": c.Value,
		})
	}
	cmds := []map[string]any{
		{
			"name":        "init",
			"description": "把这个频道绑定到一个 git 仓库工作区",
		},
		{
			"name":        "model",
			"description": "切换这个频道用的模型",
			"options":     []map[string]any{modelOption},
		},
		{
			"name":        "effort",
			"description": "切换这个频道的思考深度",
			"options": []map[string]any{{
				"type": 3, "name": "effort", "description": "思考深度档位",
				"required": true, "choices": effortChoicesJSON,
			}},
		},
		{
			"name":        "status",
			"description": "查看这个频道的工作区绑定",
		},
		{
			"name":        "unbind",
			"description": "解绑这个频道的工作区（磁盘克隆保留）",
		},
	}
	err := botREST(ctx, token, "PUT",
		fmt.Sprintf("/applications/%s/guilds/%s/commands", appID, guildID), cmds, nil)
	if err != nil {
		slog.Error("注册斜杠命令失败", "guild", guildID, "err", err)
	}
}

// interactionEvent 只解本包用得到的字段。
type interactionEvent struct {
	ID        string `json:"id"`
	Token     string `json:"token"`
	Type      int    `json:"type"` // 2=命令 3=组件 5=modal 提交
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	Member    struct {
		User struct {
			Bot bool `json:"bot"`
		} `json:"user"`
	} `json:"member"`
	Data struct {
		Name     string   `json:"name"`      // 命令名
		CustomID string   `json:"custom_id"` // 组件/modal
		Values   []string `json:"values"`    // 消息上的下拉选择
		Options  []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"options"`
		Components json.RawMessage `json:"components"`
	} `json:"data"`
}

// option 取命令入参（没有返回空串）。
func (e interactionEvent) option(name string) string {
	for _, o := range e.Data.Options {
		if o.Name == name {
			return o.Value
		}
	}
	return ""
}

// handleInteraction 分发一条 INTERACTION_CREATE：/init 命令、它的 modal
// 提交、以及分支下拉。bot 发起的忽略；不认识的不回 callback（对发起者
// 呈现为超时，与 bot 不存在无异）。
func (s *Service) handleInteraction(ctx context.Context, token string, d json.RawMessage) {
	var ev interactionEvent
	if err := json.Unmarshal(d, &ev); err != nil {
		slog.Warn("interaction 解析失败", "err", err)
		return
	}
	if ev.Member.User.Bot {
		return
	}
	switch {
	case ev.Type == 2 && ev.Data.Name == "init":
		s.openInitModal(ctx, token, ev)
	case ev.Type == 2 && ev.Data.Name == "model":
		s.setBindingOption(ctx, token, ev, "model")
	case ev.Type == 2 && ev.Data.Name == "effort":
		s.setBindingOption(ctx, token, ev, "effort")
	case ev.Type == 2 && ev.Data.Name == "status":
		s.showStatus(token, ev)
	case ev.Type == 2 && ev.Data.Name == "unbind":
		s.unbindChannel(ctx, token, ev)
	case ev.Type == 5 && ev.Data.CustomID == "init":
		s.submitInit(ctx, token, ev)
	case ev.Type == 3 && strings.HasPrefix(ev.Data.CustomID, "br:"):
		s.branchPicked(ctx, token, ev)
	}
}

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

// choice 是一条选择项（radio / string select 通吃）。
type choice struct {
	Label       string
	Value       string
	Description string
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

// pendingInit 是「表单已交、等选分支」的中途状态。
type pendingInit struct {
	in      initInput
	ev      interactionEvent
	created time.Time
}

type initInput struct {
	repo, cloneURL, agent, modelID, effort string
	// branch 空 = 默认分支；defaultBranch 用于把「选了默认」归一成空。
	branch, defaultBranch string
}

// submitInit 收表单：立即回 deferred（后面要现查分支、可能还要克隆几分钟），
// 后台探分支——多于一个分支就把回执卡编辑成分支下拉，否则直接开工。
func (s *Service) submitInit(ctx context.Context, token string, ev interactionEvent) {
	answers := parseModalSubmit(ev.Data.Components)
	repoIn := strings.TrimSpace(answers["repo_custom"])
	if repoIn == "" {
		repoIn = answers["repo_pick"]
	}
	agent, modelID, ok := strings.Cut(answers["model"], "|")
	if !ok || repoIn == "" {
		s.ephemeral(token, ev, "表单不完整（仓库没填/没选），重新 /init 一次。")
		return
	}
	name, cloneURL, err := resolveRepo(repoIn)
	if err != nil {
		s.ephemeral(token, ev, err.Error())
		return
	}
	effort := answers["effort"]
	if effort == "default" {
		effort = ""
	}

	// type 5 = deferred：先占住回执位（对全频道可见），结果稍后编辑进来。
	if err := interactionCallback(token, ev.ID, ev.Token, 5, nil); err != nil {
		slog.Error("/init deferred 回执失败", "err", err)
		return
	}

	go s.offerBranches(ctx, token, ev, initInput{
		repo: name, cloneURL: cloneURL,
		agent: agent, modelID: modelID, effort: effort,
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

// finishInit 是 /init 的慢半段：克隆（或复用）、落绑定、回写身份卡并
// 置顶、把摘要写进频道主题。
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

	old, _ := s.store.config().binding(ev.ChannelID)
	now := time.Now()
	binding := Binding{
		ChannelID: ev.ChannelID, ChannelName: channelName, GuildID: ev.GuildID,
		Repo: in.repo, CloneURL: in.cloneURL, Branch: in.branch, Workdir: workdir,
		Agent: in.agent, Model: in.modelID, ModelLabel: s.modelLabel(ctx, in.agent, in.modelID),
		Effort: in.effort, CreatedAt: now, UpdatedAt: now,
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
	s.editOriginal(token, appID, ev.Token, map[string]any{
		"embeds":           []map[string]any{bindingEmbed(binding, source)},
		"components":       []map[string]any{},
		"allowed_mentions": noMentions(),
	})

	// 身份卡置顶：拿到刚编辑的消息 id，钉住并记下来；旧卡（重绑）摘掉。
	var msg struct {
		ID string `json:"id"`
	}
	gctx, gcancel := context.WithTimeout(ctx, 5*time.Second)
	err = botREST(gctx, token, "GET",
		fmt.Sprintf("/webhooks/%s/%s/messages/@original", appID, ev.Token), nil, &msg)
	gcancel()
	if err == nil && msg.ID != "" {
		if old.CardMessageID != "" && old.CardMessageID != msg.ID {
			s.unpinMessage(ctx, token, ev.ChannelID, old.CardMessageID)
		}
		s.pinMessage(ctx, token, ev.ChannelID, msg.ID)
		binding.CardMessageID = msg.ID
		if _, err := s.store.update(func(c *Config) { c.upsertBinding(binding) }); err != nil {
			slog.Warn("身份卡 id 落盘失败", "err", err)
		}
	}
	s.syncTopic(ctx, token, binding)
}

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

// setBindingOption 处理 /model 与 /effort：改绑定、刷新置顶卡与频道主题，
// 回一条只有本人可见的确认。
func (s *Service) setBindingOption(ctx context.Context, token string, ev interactionEvent, kind string) {
	cfg := s.store.config()
	b, ok := cfg.binding(ev.ChannelID)
	if !ok {
		s.ephemeral(token, ev, "这个频道还没绑定工作区，先 /init。")
		return
	}
	var confirm string
	switch kind {
	case "model":
		agent, modelID, ok := strings.Cut(ev.option("model"), "|")
		if !ok {
			s.ephemeral(token, ev, "认不出这个模型（用命令自带的选项选）。")
			return
		}
		b.Agent, b.Model = agent, modelID
		b.ModelLabel = s.modelLabel(ctx, agent, modelID)
		confirm = "✅ 模型已切换：**" + b.ModelLabel + "**"
	case "effort":
		v := ev.option("effort")
		if v == "default" {
			v = ""
		}
		b.Effort = v
		label := v
		if label == "" {
			label = "默认"
		}
		confirm = "✅ 思考深度已切换：**" + label + "**"
	}
	b.UpdatedAt = time.Now()
	if _, err := s.store.update(func(c *Config) { c.upsertBinding(b) }); err != nil {
		s.ephemeral(token, ev, "保存失败："+trimRunes(err.Error(), 200))
		return
	}
	s.ephemeral(token, ev, confirm)
	go s.syncChannelCard(ctx, token, b)
}

// showStatus 用 /status 回一张只有本人可见的身份卡。
func (s *Service) showStatus(token string, ev interactionEvent) {
	cfg := s.store.config()
	b, ok := cfg.binding(ev.ChannelID)
	if !ok {
		s.ephemeral(token, ev, "这个频道还没绑定工作区，先 /init。")
		return
	}
	err := interactionCallback(token, ev.ID, ev.Token, 4, map[string]any{
		"embeds":           []map[string]any{bindingEmbed(b, "")},
		"flags":            1 << 6,
		"allowed_mentions": noMentions(),
	})
	if err != nil {
		slog.Error("/status 回复失败", "err", err)
	}
}

// unbindChannel 处理 /unbind：撤绑定、摘置顶卡、清频道主题。磁盘上的
// 克隆保留（里面可能有没推送的活），重新绑定就是再跑一次 /init。
func (s *Service) unbindChannel(ctx context.Context, token string, ev interactionEvent) {
	cfg := s.store.config()
	b, ok := cfg.binding(ev.ChannelID)
	if !ok {
		s.ephemeral(token, ev, "这个频道本来就没绑定工作区。")
		return
	}
	if _, err := s.store.update(func(c *Config) { c.removeBinding(ev.ChannelID) }); err != nil {
		s.ephemeral(token, ev, "解绑失败："+trimRunes(err.Error(), 200))
		return
	}
	s.ephemeral(token, ev, fmt.Sprintf("✅ 已解绑 **%s**。克隆保留在 `%s`，重新绑定用 /init。", b.Repo, b.Workdir))
	go s.cleanupChannelCard(ctx, token, b)
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

// modelLabel 反查展示名；catalog 拿不到就退回原 id。
func (s *Service) modelLabel(ctx context.Context, agent, modelID string) string {
	if s.deps.Catalog == nil {
		return modelID
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	opts, err := s.deps.Catalog(cctx)
	if err != nil {
		return modelID
	}
	for _, a := range opts {
		if a.Agent != agent {
			continue
		}
		for _, m := range a.Models {
			if m.ID == modelID {
				return a.Agent + " · " + m.Label
			}
		}
	}
	return modelID
}

func (s *Service) appID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.AppID
}

func randomID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
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

// ephemeral 回一条只有发起者可见的说明。
func (s *Service) ephemeral(token string, ev interactionEvent, text string) {
	err := interactionCallback(token, ev.ID, ev.Token, 4, map[string]any{
		"content":          text,
		"flags":            1 << 6,
		"allowed_mentions": noMentions(),
	})
	if err != nil {
		slog.Error("ephemeral 回复失败", "err", err)
	}
}

// parseModalSubmit 从提交载荷里抠答案：Label 包着的输入件在 component
// 字段，传统 action row 在 components 数组——两种都认（平台过渡期实测）。
func parseModalSubmit(raw json.RawMessage) map[string]string {
	answers := map[string]string{}
	var walk func(node json.RawMessage)
	walk = func(node json.RawMessage) {
		var n struct {
			CustomID   string            `json:"custom_id"`
			Value      string            `json:"value"`
			Values     []string          `json:"values"`
			Component  json.RawMessage   `json:"component"`
			Components []json.RawMessage `json:"components"`
		}
		if err := json.Unmarshal(node, &n); err != nil {
			return
		}
		if n.CustomID != "" {
			switch {
			case len(n.Values) > 0:
				answers[n.CustomID] = n.Values[0]
			case n.Value != "":
				answers[n.CustomID] = n.Value
			}
		}
		if len(n.Component) > 0 {
			walk(n.Component)
		}
		for _, c := range n.Components {
			walk(c)
		}
	}
	var top []json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return answers
	}
	for _, c := range top {
		walk(c)
	}
	return answers
}

func trimRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}
