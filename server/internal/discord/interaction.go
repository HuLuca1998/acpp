package discord

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Discord 品牌色（结果卡用）。
const (
	colorGreen  = 0x57F287
	colorRed    = 0xED4245
	colorBlurbe = 0x5865F2
	colorGrey   = 0x95A5A6
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
	var accessChoicesJSON []map[string]any
	for _, c := range accessChoices() {
		accessChoicesJSON = append(accessChoicesJSON, map[string]any{
			"name": c.Label + "——" + c.Description, "value": c.Value,
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
			"name":        "access",
			"description": "切换这个频道的安全权限档",
			"options": []map[string]any{{
				"type": 3, "name": "access", "description": "权限档位",
				"required": true, "choices": accessChoicesJSON,
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
		{
			"name":        "stop",
			"description": "中止子区里正在跑的回合",
		},
		{
			"name":        "help",
			"description": "acpp 使用指南",
		},
		{
			"name":        "db",
			"description": "本子区的数据库工具面开关（默认关，防止没必要的查询）",
			"options": []map[string]any{{
				"type": 3, "name": "switch", "description": "on 挂载 / off 卸载 / status 查看",
				"required": true, "choices": []map[string]any{
					{"name": "on", "value": "on"},
					{"name": "off", "value": "off"},
					{"name": "status", "value": "status"},
				},
			}},
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
			Username string `json:"username"`
			Bot      bool   `json:"bot"`
		} `json:"user"`
	} `json:"member"`
	// DM 里用户在顶层（guild 内在 member 下）。
	User struct {
		Username string `json:"username"`
	} `json:"user"`
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

// user 取发起者的展示名（问答卡收口标注「由谁处理」）。
func (e interactionEvent) user() string {
	if e.Member.User.Username != "" {
		return e.Member.User.Username
	}
	return e.User.Username
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
	case ev.Type == 2 && ev.Data.Name == "access":
		s.setBindingOption(ctx, token, ev, "access")
	case ev.Type == 2 && ev.Data.Name == "status":
		s.showStatus(token, ev)
	case ev.Type == 2 && ev.Data.Name == "unbind":
		s.unbindChannel(ctx, token, ev)
	case ev.Type == 2 && ev.Data.Name == "stop":
		s.stopThread(token, ev)
	case ev.Type == 2 && ev.Data.Name == "db":
		s.toggleDB(token, ev)
	case ev.Type == 2 && ev.Data.Name == "help":
		s.showHelp(token, ev)
	case ev.Type == 5 && ev.Data.CustomID == "init":
		s.submitInit(ctx, token, ev)
	case ev.Type == 5 && strings.HasPrefix(ev.Data.CustomID, "em:"):
		s.handleAskModal(token, ev)
	case ev.Type == 3 && strings.HasPrefix(ev.Data.CustomID, "br:"):
		s.branchPicked(ctx, token, ev)
	case ev.Type == 3 && isAskComponent(ev.Data.CustomID):
		s.handleAskComponent(token, ev)
	}
}

// isAskComponent 认问答卡的全部组件前缀（权限按钮、填表/继续按钮）。
// 新增前缀记得来这里登记——漏了就是「该 APP 未能及时响应」（实测踩过）。
func isAskComponent(customID string) bool {
	for _, p := range []string{"pm:", "eb:", "ec:"} {
		if strings.HasPrefix(customID, p) {
			return true
		}
	}
	return false
}

// bindingForCommand 给绑定类命令找归属：主频道直接查，子区落到父频道的
// 绑定上（/model /effort /access /status 在子区里也该能用）。
func (s *Service) bindingForCommand(cfg Config, channelID string) (Binding, bool) {
	if b, ok := cfg.binding(channelID); ok {
		return b, true
	}
	if t, ok := cfg.thread(channelID); ok {
		return cfg.binding(t.ChannelID)
	}
	return Binding{}, false
}

// setBindingOption 处理 /model /effort /access：改绑定、刷新频道主题，
// 回一条只有本人可见、阅后即焚的确认。子区里执行作用于父频道的绑定。
func (s *Service) setBindingOption(ctx context.Context, token string, ev interactionEvent, kind string) {
	cfg := s.store.config()
	b, ok := s.bindingForCommand(cfg, ev.ChannelID)
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
	case "access":
		v := ev.option("access")
		if accessLabel(v) == "" {
			s.ephemeral(token, ev, "认不出这个权限档（用命令自带的选项选）。")
			return
		}
		b.Access = v
		confirm = "✅ 安全权限已切换：**" + accessLabel(v) + "**"
	}
	b.UpdatedAt = time.Now()
	if _, err := s.store.update(func(c *Config) { c.upsertBinding(b) }); err != nil {
		s.ephemeral(token, ev, "保存失败："+trimRunes(err.Error(), 200))
		return
	}
	s.ephemeral(token, ev, confirm)
	go s.syncChannelCard(ctx, token, b)
}

// showStatus 用 /status 回一张只有本人可见的绑定详情卡，看完自动消失。
// 子区里执行给的是父频道的绑定。
func (s *Service) showStatus(token string, ev interactionEvent) {
	cfg := s.store.config()
	b, ok := s.bindingForCommand(cfg, ev.ChannelID)
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
		return
	}
	s.deleteOriginalLater(token, s.appID(), ev.Token)
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

// ephemeral 回一条只有发起者可见的说明，看完自动消失——确认与提示都是
// 一次性信息，常驻的事实面在频道主题与 /status。
func (s *Service) ephemeral(token string, ev interactionEvent, text string) {
	err := interactionCallback(token, ev.ID, ev.Token, 4, map[string]any{
		"content":          text,
		"flags":            1 << 6,
		"allowed_mentions": noMentions(),
	})
	if err != nil {
		slog.Error("ephemeral 回复失败", "err", err)
		return
	}
	s.deleteOriginalLater(token, s.appID(), ev.Token)
}

// ephemeralTTL 是终态 ephemeral 回执的存活时间：确认看一眼就够了，
// 到点自动清掉，不在发起者的视图里堆积。失败卡刻意不走这条路——
// 错误信息可能要复制，留到刷新自然消失。
const ephemeralTTL = time.Minute

// deleteOriginalLater 在 TTL 后删掉 interaction 的 @original 回执。
// interaction token 活 15 分钟，一分钟后删绰绰有余；删失败无所谓——
// ephemeral 本来就只有发起者可见，客户端刷新也会消失。
func (s *Service) deleteOriginalLater(token, appID, interactionToken string) {
	if appID == "" {
		return
	}
	time.AfterFunc(ephemeralTTL, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := botREST(ctx, token, "DELETE",
			fmt.Sprintf("/webhooks/%s/%s/messages/@original", appID, interactionToken), nil, nil)
		if err != nil {
			// 失败要看得见——这是排查「卡片没消失」的唯一线索。
			slog.Warn("清理 ephemeral 回执失败", "err", err)
			return
		}
		slog.Info("已清理 ephemeral 回执")
	})
}

// parseModalSubmit 从提交载荷里抠答案：Label 包着的输入件在 component
// 字段，传统 action row 在 components 数组——两种都认（平台过渡期实测）。
// 多选组件（CheckboxGroup/多选下拉）一名多值，答案统一是集合。
func parseModalSubmit(raw json.RawMessage) map[string][]string {
	answers := map[string][]string{}
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
				answers[n.CustomID] = n.Values
			case strings.TrimSpace(n.Value) != "":
				answers[n.CustomID] = []string{strings.TrimSpace(n.Value)}
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

// firstAnswer 取一个字段的首个值（单值组件用）。
func firstAnswer(answers map[string][]string, key string) string {
	if vs := answers[key]; len(vs) > 0 {
		return vs[0]
	}
	return ""
}

func trimRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}

// showHelp 是 /help：只有本人可见，不自动删——说明书要留着慢慢看。
func (s *Service) showHelp(token string, ev interactionEvent) {
	text := "## acpp 使用指南\n" +
		"**开始对话** — 在绑定频道 @acpp 说话（@ 出来选用户或角色都行），自动开子区；之后在子区里直接说话。\n" +
		"**发文件** — 消息附件直接进对话：图片给模型看，文本嵌全文，大文件落盘给路径。\n" +
		"**查数据库** — 默认不连库；`/db on` 或消息里带 `@db` 挂载数据库工具，`/db off` 卸载。\n" +
		"**要报告** — 说「写一份 xx 报告并打开」，出报告卡一键浏览器预览。\n" +
		"**回合中** — ⏳ 已排队、✅ 已进对话；权限/提问是卡片，点按钮或直接回话（选项可回编号）。\n" +
		"**常用命令** — `/model` `/effort` `/access` 调模型与权限档；`/status` 看绑定；`/stop` 中止本轮；`/unbind` 解绑；`/init` 绑定频道。"
	if err := interactionCallback(token, ev.ID, ev.Token, 4, map[string]any{
		"content": text, "flags": 1 << 6, "allowed_mentions": noMentions(),
	}); err != nil {
		slog.Error("help 回复失败", "err", err)
	}
}
