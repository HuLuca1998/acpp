package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
)

// agentCatalog 是配置页取舍的快照：被禁用的模型与命令。
type agentCatalog struct {
	models   map[string]bool
	aliases  map[string]string
	commands map[string]bool
	fastOff  bool
}

// catalogFor 读会话所属 agent 的配置页取舍。读不到按全启用处理——
// 过滤是体验优化，不该因为一次查库失败把清单清空。
func (s *ChatService) catalogFor(ctx context.Context, sessionID uint) agentCatalog {
	cat := agentCatalog{
		models:   map[string]bool{},
		aliases:  map[string]string{},
		commands: map[string]bool{},
	}
	var session model.Session
	if err := s.db.WithContext(ctx).First(&session, sessionID).Error; err != nil {
		return cat
	}
	var agent model.Agent
	if err := s.db.WithContext(ctx).First(&agent, session.AgentID).Error; err != nil {
		return cat
	}
	for _, m := range agent.Models {
		if m.Disabled {
			cat.models[m.ID] = true
		}
		if m.Alias != "" {
			cat.aliases[m.ID] = m.Alias
		}
	}
	for _, c := range agent.Commands {
		if c.Disabled {
			cat.commands[c.Name] = true
		}
	}
	cat.fastOff = agent.FastPolicy == "off"
	return cat
}

// filterSettings 应用配置页取舍：禁用的模型从清单去掉（当前值不动——
// 就算被禁也如实显示）、别名替换显示名、快速模式关闭时抹掉支持位。
func (cat agentCatalog) filterSettings(settings *acp.Settings) {
	if settings == nil {
		return
	}
	kept := make([]acp.Model, 0, len(settings.Models))
	for _, m := range settings.Models {
		if cat.models[m.ID] {
			continue
		}
		if alias := cat.aliases[m.ID]; alias != "" {
			m.Name = alias
		}
		kept = append(kept, m)
	}
	settings.Models = kept
	if cat.fastOff {
		settings.FastSupported = false
		settings.FastOn = false
	}
}

// filterCommands 去掉配置页禁用的斜杠命令。
func (cat agentCatalog) filterCommands(commands []acp.Command) []acp.Command {
	if len(cat.commands) == 0 {
		return commands
	}
	kept := make([]acp.Command, 0, len(commands))
	for _, c := range commands {
		if !cat.commands[c.Name] {
			kept = append(kept, c)
		}
	}
	return kept
}

// DegradedSettings 用 agent 探测缓存与会话最后设置快照拼出未连接时的
// 设置视图。切片给空值而不是 nil——JSON null 会让前端 .length 直接崩。
func DegradedSettings(agent *model.Agent, last model.JSONMap) *acp.Settings {
	settings := &acp.Settings{
		Flavor:        acp.Flavor(agent.Flavor),
		Models:        []acp.Model{},
		Efforts:       []acp.Effort{},
		Levels:        []acp.AccessLevel{},
		PlanSupported: agent.Skeleton.PlanSupported,
		FastSupported: agent.Skeleton.FastSupported && agent.FastPolicy != "off",
	}
	for _, m := range agent.Models {
		if m.Disabled {
			continue
		}
		name := m.Name
		if m.Alias != "" {
			name = m.Alias
		}
		settings.Models = append(settings.Models, acp.Model{
			ID: m.ID, Name: name, Description: m.Description,
		})
	}
	for _, e := range agent.Skeleton.Efforts {
		settings.Efforts = append(settings.Efforts, acp.Effort(e))
	}
	for _, l := range agent.Skeleton.Levels {
		settings.Levels = append(settings.Levels, acp.AccessLevel(l))
	}
	str := func(k string) string {
		v, _ := last[k].(string)
		return v
	}
	boolean := func(k string) bool {
		v, _ := last[k].(bool)
		return v
	}
	settings.CurrentModel = str("model")
	settings.CurrentEffort = acp.Effort(str("effort"))
	settings.CurrentLevel = acp.AccessLevel(str("level"))
	settings.PlanOn = boolean("plan")
	settings.FastOn = boolean("fast")
	return settings
}

// saveSettingsSnapshot 把统一设置的当前值写回会话记录（旁路，失败只记日志），
// 供恢复会话时的降级视图展示与连接时一致的当前值。
func (s *ChatService) saveSettingsSnapshot(sessionID uint, settings *acp.Settings) {
	snapshot := model.JSONMap{
		"model":  settings.CurrentModel,
		"effort": string(settings.CurrentEffort),
		"level":  string(settings.CurrentLevel),
		"plan":   settings.PlanOn,
		"fast":   settings.FastOn,
	}
	if err := s.db.Model(&model.Session{}).
		Where("id = ?", sessionID).Update("last_settings", snapshot).Error; err != nil {
		slog.Warn("save settings snapshot", "session", sessionID, "err", err)
	}
}

// settingsRestorePatch 算出把 current 拨回快照 last 需要下发的那几项。
//
// 一项要三样都成立才下发：快照里有、与当前值不同、且新进程确实支持它——
// Apply 是逐项串行、遇错即停的，混进一个已经下线的模型 id 会让排在它后面
// 的档位统统不生效。
func settingsRestorePatch(current acp.Settings, last model.JSONMap) (acp.SettingsPatch, bool) {
	var patch acp.SettingsPatch
	changed := false

	if v, _ := last["model"].(string); v != "" && v != current.CurrentModel &&
		slices.ContainsFunc(current.Models, func(m acp.Model) bool { return m.ID == v }) {
		patch.Model = &v
		changed = true
	}
	if v, _ := last["effort"].(string); v != "" && acp.Effort(v) != current.CurrentEffort &&
		slices.Contains(current.Efforts, acp.Effort(v)) {
		e := acp.Effort(v)
		patch.Effort = &e
		changed = true
	}
	if v, _ := last["level"].(string); v != "" && acp.AccessLevel(v) != current.CurrentLevel &&
		slices.Contains(current.Levels, acp.AccessLevel(v)) {
		l := acp.AccessLevel(v)
		patch.Level = &l
		changed = true
	}
	if v, ok := last["plan"].(bool); ok && current.PlanSupported && v != current.PlanOn {
		patch.Plan = &v
		changed = true
	}
	if v, ok := last["fast"].(bool); ok && current.FastSupported && v != current.FastOn {
		patch.Fast = &v
		changed = true
	}
	return patch, changed
}

// restoreSettings 把会话最后一次生效的设置回放到刚拉起的子进程。
//
// 模型、思考深度、权限档都是 ACP 会话级的运行时状态，跟着子进程一起死：
// 空闲回收（或崩溃）后重开，agent 给回来的是它自己的默认值。不回放的话，
// 用户离开十分钟再发一条消息，模型和权限档就自己变了；更糟的是这份默认值
// 随即被 saveSettingsSnapshot 写回快照，把用户设过的值永久冲掉。
//
// 只补差异项：session/load 可能已经恢复了一部分，重设一遍是白跑的 RPC。
// 失败只记日志——设置没拨回来是遗憾，会话开不起来才是事故。
//
// 返回值是「快照现在可以安全覆盖了吗」：拨回来了、或者压根没什么要拨的，
// 都是 true；只有真的拨失败才是 false，此时调用方必须留着旧快照——拿默认值
// 盖上去，用户设过的值就再也找不回来了。
func (s *ChatService) restoreSettings(ctx context.Context, sessionID uint, last model.JSONMap) bool {
	if len(last) == 0 {
		return true
	}
	key := sessionKey(sessionID)
	current, err := s.manager.Settings(key)
	if err != nil {
		return true
	}
	patch, ok := settingsRestorePatch(current, last)
	if !ok {
		return true
	}
	if _, err := s.manager.Apply(ctx, key, patch); err != nil {
		slog.Warn("restore session settings", "session", sessionID, "err", err)
		return false
	}
	return true
}

// ApplySettings 逐项应用统一设置变更并广播最新视图（多标签页保持同步）。
func (s *ChatService) ApplySettings(ctx context.Context, sessionID uint, patch acp.SettingsPatch) (*acp.Settings, error) {
	// 老会话未连接时也允许直接改设置——先幂等拉起进程再应用，
	// 用户不需要「先发一条消息」才能换模型。
	if _, err := s.Open(ctx, sessionID); err != nil {
		return nil, err
	}
	settings, err := s.manager.Apply(ctx, sessionKey(sessionID), patch)
	if err != nil {
		return nil, translateNoSession(sessionID, err)
	}
	s.catalogFor(ctx, sessionID).filterSettings(&settings)
	s.saveSettingsSnapshot(sessionID, &settings)
	s.brokerFor(sessionID).Publish(StreamEvent{Kind: "settings", Settings: &settings})
	return &settings, nil
}

func translateNoSession(sessionID uint, err error) error {
	if errors.Is(err, acp.ErrNoSession) {
		return fmt.Errorf("session %d: %w", sessionID, ErrNotFound)
	}
	return err
}
