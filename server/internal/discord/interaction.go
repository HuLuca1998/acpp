package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
)

// Discord 品牌色（结果卡用）。
const (
	colorGreen = 0x57F287
	colorRed   = 0xED4245
)

// registerCommands 注册 guild 级斜杠命令（即时生效；global 有传播延迟）。
// PUT 语义是全量覆盖，幂等，每次连接对每个 guild 执行一遍。
func (s *Service) registerCommands(ctx context.Context, token, appID, guildID string) {
	cmds := []map[string]any{{
		"name":        "init",
		"description": "把这个频道绑定到一个 git 仓库工作区",
	}}
	err := botREST(ctx, token, "PUT",
		fmt.Sprintf("/applications/%s/guilds/%s/commands", appID, guildID), cmds, nil)
	if err != nil {
		slog.Error("注册 /init 失败", "guild", guildID, "err", err)
	}
}

// interactionEvent 只解本包用得到的字段。
type interactionEvent struct {
	ID        string `json:"id"`
	Token     string `json:"token"`
	Type      int    `json:"type"` // 2=命令 5=modal 提交
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	Member    struct {
		User struct {
			Bot bool `json:"bot"`
		} `json:"user"`
	} `json:"member"`
	Data struct {
		Name       string          `json:"name"`      // 命令名
		CustomID   string          `json:"custom_id"` // modal
		Components json.RawMessage `json:"components"`
	} `json:"data"`
}

// handleInteraction 分发一条 INTERACTION_CREATE：目前只有 /init 与它的
// modal 提交。bot 发起的忽略；不认识的不回 callback（对发起者呈现为超时，
// 与 bot 不存在无异）。
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
	case ev.Type == 5 && ev.Data.CustomID == "init":
		s.submitInit(ctx, token, ev)
	}
}

// openInitModal 弹出绑定表单：仓库（预填现有绑定）+ 模型 + 思考深度。
// callback 有 3 秒时限，catalog 是本地库读，来得及。
func (s *Service) openInitModal(ctx context.Context, token string, ev interactionEvent) {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var catalog []AgentOption
	if s.catalog != nil {
		if opts, err := s.catalog(cctx); err == nil {
			catalog = opts
		}
	}
	models := modelChoices(catalog)
	if len(models) == 0 {
		s.ephemeral(token, ev, "模型清单还没就绪——先在 acpp 设置页完成内置工具探测，再回来 /init。")
		return
	}

	current, _ := s.store.config().binding(ev.ChannelID)
	repoInput := map[string]any{
		"type": 4, "custom_id": "repo", "style": 1, "required": true,
		"placeholder": "BDBGAME2024/pp-game 或完整 clone 地址",
	}
	if current.Repo != "" {
		repoInput["value"] = current.Repo
	}

	comps := []map[string]any{
		{"type": 18, "label": "Git 仓库", "description": "owner/repo 简写按 GitHub 解析", "component": repoInput},
		{"type": 18, "label": "模型", "component": selectComponent("model", models)},
		{"type": 18, "label": "思考深度", "component": selectComponent("effort", effortChoices())},
	}
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
func selectComponent(customID string, choices []choice) map[string]any {
	kind := 21
	if len(choices) > 10 {
		kind = 3
	}
	opts := make([]map[string]any, 0, len(choices))
	for _, c := range choices {
		opt := map[string]any{"label": trimRunes(c.Label, 90), "value": c.Value}
		if c.Description != "" {
			opt["description"] = trimRunes(c.Description, 90)
		}
		opts = append(opts, opt)
	}
	out := map[string]any{"type": kind, "custom_id": customID, "options": opts, "required": true}
	if kind == 3 {
		out["placeholder"] = "选一个…"
	}
	return out
}

// submitInit 收表单：立即回 deferred（克隆可能要几分钟），后台克隆 +
// 落绑定，再把结果卡写回频道。
func (s *Service) submitInit(ctx context.Context, token string, ev interactionEvent) {
	answers := parseModalSubmit(ev.Data.Components)
	repoIn, modelIn, effortIn := answers["repo"], answers["model"], answers["effort"]

	agent, modelID, ok := strings.Cut(modelIn, "|")
	if !ok || repoIn == "" {
		s.ephemeral(token, ev, "表单不完整，重新 /init 一次。")
		return
	}
	name, cloneURL, err := resolveRepo(repoIn)
	if err != nil {
		s.ephemeral(token, ev, err.Error())
		return
	}
	if effortIn == "default" {
		effortIn = ""
	}

	// type 5 = deferred：先占住回执位（对全频道可见），结果稍后编辑进来。
	if err := interactionCallback(token, ev.ID, ev.Token, 5, nil); err != nil {
		slog.Error("/init deferred 回执失败", "err", err)
		return
	}

	go s.finishInit(ctx, token, ev, initInput{
		repo: name, cloneURL: cloneURL,
		agent: agent, modelID: modelID, effort: effortIn,
	})
}

type initInput struct {
	repo, cloneURL, agent, modelID, effort string
}

// finishInit 是 /init 的慢半段：克隆（或复用）、落绑定、回写结果卡。
func (s *Service) finishInit(ctx context.Context, token string, ev interactionEvent, in initInput) {
	appID := s.appID()
	workdir := filepath.Join(effectiveWorkRoot(s.store.config()), filepath.FromSlash(in.repo))

	reused, err := ensureWorkdir(ctx, in.cloneURL, workdir)
	if err != nil {
		s.editOriginal(token, appID, ev.Token, map[string]any{
			"embeds": []map[string]any{{
				"title":       "❌ 工作区创建失败",
				"description": fmt.Sprintf("**%s**\n```\n%s\n```", in.repo, trimRunes(err.Error(), 900)),
				"color":       colorRed,
			}},
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

	modelLabel := s.modelLabel(ctx, in.agent, in.modelID)
	now := time.Now()
	_, err = s.store.update(func(c *Config) {
		c.upsertBinding(Binding{
			ChannelID: ev.ChannelID, ChannelName: channelName, GuildID: ev.GuildID,
			Repo: in.repo, CloneURL: in.cloneURL, Workdir: workdir,
			Agent: in.agent, Model: in.modelID, ModelLabel: modelLabel, Effort: in.effort,
			CreatedAt: now, UpdatedAt: now,
		})
	})
	if err != nil {
		slog.Error("绑定落盘失败", "channel", ev.ChannelID, "err", err)
		s.editOriginal(token, appID, ev.Token, map[string]any{
			"embeds": []map[string]any{{
				"title": "❌ 绑定保存失败", "description": trimRunes(err.Error(), 900), "color": colorRed,
			}},
			"allowed_mentions": noMentions(),
		})
		return
	}

	source := "已克隆到"
	if reused {
		source = "复用已有克隆"
	}
	effort := in.effort
	if effort == "" {
		effort = "默认"
	}
	s.editOriginal(token, appID, ev.Token, map[string]any{
		"embeds": []map[string]any{{
			"title": "✅ 频道工作区已就绪",
			"description": fmt.Sprintf("**%s**\n%s `%s`\n模型 **%s** · 思考深度 **%s**\n之后这个频道的工作目录就是它。",
				in.repo, source, workdir, modelLabel, effort),
			"color": colorGreen,
		}},
		"allowed_mentions": noMentions(),
	})
}

// modelLabel 反查展示名；catalog 拿不到就退回原 id。
func (s *Service) modelLabel(ctx context.Context, agent, modelID string) string {
	if s.catalog == nil {
		return modelID
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	opts, err := s.catalog(cctx)
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
