package discord

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
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
	// 表单到此为止：modal 最多 5 个组件（多一个就是 400
	// BASE_TYPE_MAX_LENGTH，实测），所以「数据库」挪到了分支之后的一步，
	// 见 offerDataSource。

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

// dbChoices 是数据库选项：`<项目>/<环境>` 打头，描述里带库名与读写状态。
// 当前已绑的排最前（重绑时一眼能看到原来选的是谁），「不锁定」兜底在最后。
// value 编码 `<id>|<ref>`，提交时拆回——ref 一起带上，落盘的展示快照就不用
// 再查一次库。
func dbChoices(dbs []DBOption, current uint) []choice {
	var out []choice
	for _, d := range dbs {
		mode := "可写"
		if d.ReadOnly {
			mode = "只读"
		}
		label := d.Ref
		if d.ID == current {
			label += "（当前）"
		}
		c := choice{
			Label:       label,
			Value:       fmt.Sprintf("%d|%s", d.ID, d.Ref),
			Description: d.Database + " · " + mode,
		}
		if d.ID == current {
			out = append([]choice{c}, out...)
			continue
		}
		out = append(out, c)
	}
	out = append(out, choice{
		Label: "不锁定", Value: dbNoneValue,
		Description: "本频道所在项目的数据源全部可见（老口径）",
	})
	if len(out) > 25 {
		out = out[:25]
	}
	return out
}

// dbOptionByRef 按 `<项目>/<环境>` 在当前清单里找一条数据源——给手输
// （而不是从命令选项里选）的 /db source 兜底。
func (s *Service) dbOptionByRef(ctx context.Context, ref string) (DBOption, bool) {
	ref = strings.TrimSpace(ref)
	if s.deps.DataSources == nil || ref == "" {
		return DBOption{}, false
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	list, err := s.deps.DataSources(cctx)
	if err != nil {
		slog.Warn("取数据源清单失败", "err", err)
		return DBOption{}, false
	}
	for _, d := range list {
		if strings.EqualFold(d.Ref, ref) {
			return d, true
		}
	}
	return DBOption{}, false
}

// serverOptionByName 按名字在当前清单里找一台服务器——给手输（而不是从
// 命令选项里选）的 /server 兜底。
func (s *Service) serverOptionByName(ctx context.Context, name string) (ServerOption, bool) {
	name = strings.TrimSpace(name)
	if s.deps.Servers == nil || name == "" {
		return ServerOption{}, false
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	list, err := s.deps.Servers(cctx)
	if err != nil {
		slog.Warn("取服务器清单失败", "err", err)
		return ServerOption{}, false
	}
	for _, h := range list {
		if strings.EqualFold(h.Name, name) {
			return h, true
		}
	}
	return ServerOption{}, false
}

// dbNoneValue 是「不锁定」那一项的 value（不是数据源 id，服务器那边共用）。
const dbNoneValue = "none"

// serverChoices 是服务器选项：名字打头，描述里给地址与用途备注。
// 与 dbChoices 同构——当前绑定的那台排在最前并标注。
func serverChoices(hosts []ServerOption, current uint) []choice {
	var out []choice
	for _, h := range hosts {
		label := h.Name
		if h.ID == current {
			label += "（当前）"
		}
		desc := h.Host
		if note := strings.TrimSpace(h.Note); note != "" {
			desc += " · " + note
		}
		c := choice{
			Label:       label,
			Value:       fmt.Sprintf("%d|%s", h.ID, h.Name),
			Description: trimRunes(desc, 90),
		}
		if h.ID == current {
			out = append([]choice{c}, out...)
			continue
		}
		out = append(out, c)
	}
	out = append(out, choice{
		Label: "不锁定", Value: dbNoneValue,
		Description: "全部启用的服务器都可见（老口径）",
	})
	if len(out) > 25 {
		out = out[:25]
	}
	return out
}

// parseServerChoice 与 parseDBChoice 同形：`<id>|<名字>`，认不出按不锁定。
func parseServerChoice(v string) (uint, string) {
	id, name := parseDBChoice(v)
	return id, name
}

// parseDBChoice 拆 `<id>|<ref>`；「不锁定」与认不出的输入都归零值。
func parseDBChoice(v string) (uint, string) {
	idStr, ref, ok := strings.Cut(v, "|")
	if !ok {
		return 0, ""
	}
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || id == 0 {
		return 0, ""
	}
	return uint(id), ref
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

// offerBranches 现查远端分支，让用户选**基础分支**（频道的工作分支从它切
// 出来，见 ensureWorktree）。只有一个（或查不动）就按默认分支继续。
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
		s.offerDataSource(ctx, token, ev, in)
		return
	}

	id, perr := randomID()
	if perr != nil {
		s.offerDataSource(ctx, token, ev, in)
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
	sel["placeholder"] = "选择基础分支…"
	delete(sel, "required")

	s.editOriginal(token, appID, ev.Token, map[string]any{
		"embeds": []map[string]any{{
			"title": "选择基础分支",
			"description": fmt.Sprintf(
				"**%s** 有 %d 个分支。频道会从选中的这条切一条自己的工作分支——直接在 pre/prod 上干活提交推不上去。\n15 分钟内有效，过期请重新 /init。",
				in.repo, len(branches)),
			"color": colorBlurbe,
		}},
		"components":       []map[string]any{{"type": 1, "components": []map[string]any{sel}}},
		"allowed_mentions": noMentions(),
	})
}

// branchPicked 收基础分支的选择：卡片原地改成进行中，然后进入选库那一步。
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
			"description": fmt.Sprintf("**%s** · 基于 **%s**", in.repo, label),
			"color":       colorBlurbe,
		}},
		"components":       []map[string]any{},
		"allowed_mentions": noMentions(),
	})
	if err != nil {
		slog.Error("分支选择改卡失败", "err", err)
	}
	// 收尾仍编辑最初 /init 的回执（同一条消息，用建卡那次的 token）。
	go s.offerDataSource(ctx, token, p.ev, in)
}

// offerDataSource 是绑定的最后一步：选这个频道锁定哪个库。
//
// 它不在 /init 表单里，是因为 modal 最多放 5 个组件（第 6 个直接 400
// BASE_TYPE_MAX_LENGTH，真机实测），仓库/自定义仓库/模型/深度/权限已经占满。
// 没配数据源就跳过这一步，直接开工。
func (s *Service) offerDataSource(ctx context.Context, token string, ev interactionEvent, in initInput) {
	var dbs []DBOption
	if s.deps.DataSources != nil {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		list, err := s.deps.DataSources(cctx)
		cancel()
		if err != nil {
			slog.Warn("取数据源清单失败，跳过锁定这一步", "err", err)
		} else {
			dbs = list
		}
	}
	if len(dbs) == 0 {
		s.finishInit(ctx, token, ev, in)
		return
	}
	id, perr := randomID()
	if perr != nil {
		s.finishInit(ctx, token, ev, in)
		return
	}
	s.mu.Lock()
	s.pending[id] = pendingInit{in: in, ev: ev, created: time.Now()}
	s.mu.Unlock()

	current, _ := s.store.config().binding(ev.ChannelID)
	sel := selectComponent("db:"+id, dbChoices(dbs, current.DataSourceID), false)
	sel["type"] = 3 // 消息上的下拉只有 String Select
	sel["placeholder"] = "选择这个频道能查的库…"
	delete(sel, "required")

	branch := in.branch
	if branch == "" {
		branch = in.defaultBranch + "（默认）"
	}
	s.editOriginal(token, s.appID(), ev.Token, map[string]any{
		"embeds": []map[string]any{{
			"title": "选择数据库",
			"description": fmt.Sprintf(
				"**%s**（基于 %s）\n选中之后，这个频道的 AI 只看得见这一条连接——同项目别的环境列都列不出来。\n15 分钟内有效。",
				in.repo, branch),
			"color": colorBlurbe,
		}},
		"components":       []map[string]any{{"type": 1, "components": []map[string]any{sel}}},
		"allowed_mentions": noMentions(),
	})
}

// dbPicked 收数据库下拉的选择，然后进入真正的收尾（克隆/建树/落绑定）。
func (s *Service) dbPicked(ctx context.Context, token string, ev interactionEvent) {
	id := strings.TrimPrefix(ev.Data.CustomID, "db:")
	s.mu.Lock()
	p, ok := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if !ok || len(ev.Data.Values) == 0 {
		s.ephemeral(token, ev, "这张卡过期了（或已处理过），重新 /init 一次。")
		return
	}
	in := p.in
	in.dbID, in.dbRef = parseDBChoice(ev.Data.Values[0])

	label := in.dbRef
	if in.dbID == 0 {
		label = "不锁定（项目下全部环境）"
	}
	err := interactionCallback(token, ev.ID, ev.Token, 7, map[string]any{
		"embeds": []map[string]any{{
			"title":       "⏳ 正在准备工作区…",
			"description": fmt.Sprintf("**%s** · 数据库 **%s**", in.repo, label),
			"color":       colorBlurbe,
		}},
		"components":       []map[string]any{},
		"allowed_mentions": noMentions(),
	})
	if err != nil {
		slog.Error("数据库选择改卡失败", "err", err)
	}
	go s.offerServer(ctx, token, p.ev, in)
}

// offerServer 是选服务器那一步（adr-019），排在选库之后。
//
// 与数据库同一个道理：一个项目的 prod / pre 频道各自只该看见自己那台机器。
// 没配服务器就跳过——让用户对着空列表发呆没有意义。
func (s *Service) offerServer(ctx context.Context, token string, ev interactionEvent, in initInput) {
	var hosts []ServerOption
	if s.deps.Servers != nil {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		list, err := s.deps.Servers(cctx)
		cancel()
		if err != nil {
			slog.Warn("取服务器清单失败，跳过锁定这一步", "err", err)
		} else {
			hosts = list
		}
	}
	if len(hosts) == 0 {
		s.finishInit(ctx, token, ev, in)
		return
	}
	id, perr := randomID()
	if perr != nil {
		s.finishInit(ctx, token, ev, in)
		return
	}
	s.mu.Lock()
	s.pending[id] = pendingInit{in: in, ev: ev, created: time.Now()}
	s.mu.Unlock()

	current, _ := s.store.config().binding(ev.ChannelID)
	sel := selectComponent("srv:"+id, serverChoices(hosts, current.ServerID), false)
	sel["type"] = 3
	sel["placeholder"] = "选择这个频道能看的机器…"
	delete(sel, "required")

	dbLabel := in.dbRef
	if in.dbID == 0 {
		dbLabel = "不锁定"
	}
	s.editOriginal(token, s.appID(), ev.Token, map[string]any{
		"embeds": []map[string]any{{
			"title": "选择服务器",
			"description": fmt.Sprintf(
				"**%s** · 数据库 **%s**\n选中之后，这个频道的 AI 只看得见这一台机器——别的机器连列都列不出来。\n15 分钟内有效。",
				in.repo, dbLabel),
			"color": colorBlurbe,
		}},
		"components":       []map[string]any{{"type": 1, "components": []map[string]any{sel}}},
		"allowed_mentions": noMentions(),
	})
}

// serverPicked 收服务器下拉的选择，然后进入真正的收尾。
func (s *Service) serverPicked(ctx context.Context, token string, ev interactionEvent) {
	id := strings.TrimPrefix(ev.Data.CustomID, "srv:")
	s.mu.Lock()
	p, ok := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if !ok || len(ev.Data.Values) == 0 {
		s.ephemeral(token, ev, "这张卡过期了（或已处理过），重新 /init 一次。")
		return
	}
	in := p.in
	in.srvID, in.srvName = parseServerChoice(ev.Data.Values[0])

	label := in.srvName
	if in.srvID == 0 {
		label = "不锁定（全部机器）"
	}
	err := interactionCallback(token, ev.ID, ev.Token, 7, map[string]any{
		"embeds": []map[string]any{{
			"title":       "⏳ 正在准备工作区…",
			"description": fmt.Sprintf("**%s** · 服务器 **%s**", in.repo, label),
			"color":       colorBlurbe,
		}},
		"components":       []map[string]any{},
		"allowed_mentions": noMentions(),
	})
	if err != nil {
		slog.Error("服务器选择改卡失败", "err", err)
	}
	go s.finishInit(ctx, token, p.ev, in)
}

// finishInit 是 /init 的慢半段：克隆（或复用）、落绑定、把频道主题刷成
// 最新摘要（频道侧唯一常驻信息面），ephemeral 回执给一句结论后阅后即焚。
func (s *Service) finishInit(ctx context.Context, token string, ev interactionEvent, in initInput) {
	appID := s.appID()
	// 频道名是展示锦上添花，也是自动分支名的素材，查不到不挡流程。
	channelName := ""
	var ch struct {
		Name string `json:"name"`
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if err := botREST(cctx, token, "GET", "/channels/"+ev.ChannelID, nil, &ch); err == nil {
		channelName = ch.Name
	}
	cancel()
	// old 是上一次的绑定：CardMessageID 要继承（清历史遗留的置顶卡），
	// 工作分支也要尽量复用。
	old, _ := s.store.config().binding(ev.ChannelID)

	// 工作目录是「项目目录下这个频道自己那条分支的工作树」（adr-018）：一个
	// 仓库一份 git 数据，一个分支一棵树。频道不直接工作在选中的分支上——
	// 那条是 base，pre/prod 这类有保护规则，提交推不上去。
	home := repoHome(s.effectiveWorkRoot(s.store.config()), in.repo)
	base := in.branch
	if base == "" {
		base = in.defaultBranch
	}
	// 重绑（换模型/换库）复用上次那条工作分支，别每次 /init 都换一条新的；
	// 换了仓库或换了 base 就重新生成。
	reuse := ""
	if old.Repo == in.repo && old.Base == base {
		reuse = old.Branch
	}
	tree, err := ensureWorktree(ctx, worktreeSpec{
		CloneURL: in.cloneURL, Home: home,
		Branch: reuse, NameHint: channelName, Base: base,
	})
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

	now := time.Now()
	binding := Binding{
		ChannelID: ev.ChannelID, ChannelName: channelName, GuildID: ev.GuildID,
		Repo: in.repo, CloneURL: in.cloneURL, Branch: tree.Branch, Base: tree.Base, Workdir: tree.Dir,
		Agent: in.agent, Model: in.modelID, ModelLabel: s.modelLabel(ctx, in.agent, in.modelID),
		Effort: in.effort, Access: in.access,
		DataSourceID: in.dbID, DataSourceRef: in.dbRef,
		ServerID: in.srvID, ServerName: in.srvName,
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

	source := "已建工作树"
	if tree.Reused {
		source = "复用已有工作树"
	}
	if tree.RetiredLegacy != "" {
		// 老布局的克隆占着项目目录，已挪开——里面可能有没推送的活，
		// 必须让用户知道它去哪了。
		source += fmt.Sprintf("；老克隆已挪到 `%s`", tree.RetiredLegacy)
	}
	s.syncChannelCard(ctx, token, binding)
	// 频道置顶使用手册：新成员第一眼能看懂怎么用。
	s.publishGuide(ctx, token, binding)
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
	// dbID/dbRef 是锁定的数据源（0 表示不锁定，维持项目全量可见）。
	dbID  uint
	dbRef string
	// srvID/srvName 是锁定的服务器（0 表示不锁定，全部启用的机器都可见）。
	srvID   uint
	srvName string
	// branch 空 = 默认分支；defaultBranch 用于把「选了默认」归一成空。
	branch, defaultBranch string
}
