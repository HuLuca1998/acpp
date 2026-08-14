package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
)

// flavorOfAgent 判断 runtime 方言：优先探测缓存，缺失时按命令名兜底。
// 注入口选择（systemPrompt vs CODEX_HOME）依赖它。
func flavorOfAgent(agent *model.Agent) string {
	if agent.Flavor != "" {
		return agent.Flavor
	}
	s := strings.ToLower(agent.Name + " " + agent.Command)
	switch {
	case strings.Contains(s, "claude"):
		return "claude"
	case strings.Contains(s, "codex"):
		return "codex"
	}
	return "generic"
}

// buildOrchPrompt 组装主会话的调度提示词：spawn_agent 用法 + 角色雇佣
// 目录。写给 AI 看——task 必须自包含是子代理没有上下文的直接推论。
func buildOrchPrompt(roles []model.Role) string {
	var b strings.Builder
	b.WriteString("\n# 编排模式\n\n")
	b.WriteString("你是编排主控，可以通过 acpp 提供的 MCP 工具 `spawn_agent` 雇佣角色子代理完成子任务。\n\n")
	b.WriteString("## 可用角色（role 参数填名字）\n\n")
	if len(roles) == 0 {
		b.WriteString("（当前没有可用角色，spawn_agent 不可用——自己完成任务。）\n")
	}
	for _, r := range roles {
		fmt.Fprintf(&b, "- **%s**：%s\n", r.Name, r.Description)
	}
	b.WriteString(`
## 委派规则

- task 必须自包含：写清背景、目标、涉及路径与验收标准——子代理看不到你的对话上下文。
- spawn_agent 会阻塞到子代理完成并返回其最终结论；需要并行时在同一条消息里发多个 spawn_agent 调用。
- 子代理彼此独立、不共享记忆，跨子任务的协调与结果整合由你负责。
- 调用失败会返回错误文本：可以重试、换角色或自己处理，不要静默放弃任务。
- 简单问题直接自己回答，不要为聊天消息雇佣子代理。
`)
	return b.String()
}

// orchInjection 是一次编排注入的组装结果，直接映射 acp.OpenOptions 的
// 三个注入口。
type orchInjection struct {
	ExtraEnv   map[string]string
	MetaExtra  map[string]any
	MCPServers []any
}

// mainInjection 组装主会话注入。两端通道不同：
//   - claude：session/new 传 MCP server + _meta（调度提示词 append、
//     disallowedTools 收掉内部 Task 子代理）+ 进程 env 放大 MCP 工具超时
//     （默认约 2 分钟，实测扛不住长子任务）。
//   - codex：一切走专属 CODEX_HOME——config.toml 定义 MCP server（含
//     tool_timeout_sec），AGENTS.md 承载调度提示词；session/new 不传
//     mcpServers（config 已定义，传两遍会重名）。
func (s *OrchService) mainInjection(orch *model.OrchSession, agent *model.Agent, prompt string) (orchInjection, error) {
	mcpURL := s.mcpBase + orch.MCPToken
	switch flavorOfAgent(agent) {
	case "claude":
		return orchInjection{
			ExtraEnv: map[string]string{
				"MCP_TOOL_TIMEOUT": strconv.Itoa(mcpToolTimeoutMS),
			},
			MetaExtra: map[string]any{
				"systemPrompt": map[string]any{"append": prompt},
				"claudeCode": map[string]any{"options": map[string]any{
					"disallowedTools": []string{"Task"},
				}},
			},
			MCPServers: []any{map[string]any{
				"type":    "http",
				"name":    "acpp",
				"url":     mcpURL,
				"headers": []any{},
			}},
		}, nil
	case "codex":
		home := filepath.Join(s.dataDir, "orch", fmt.Sprintf("home-%d", orch.ID))
		if err := s.ensureOrchCodexHome(home, prompt, &mcpURL); err != nil {
			return orchInjection{}, fmt.Errorf("build orch codex home: %w", err)
		}
		return orchInjection{
			ExtraEnv: map[string]string{"CODEX_HOME": home},
		}, nil
	}
	// generic runtime 没有可靠的注入口：不挂 MCP、不注提示词，
	// 编排会话退化成普通对话（创建时已挡，这里兜底）。
	return orchInjection{}, nil
}

// roleInjection 组装角色子会话注入：persona 进 claude 的 systemPrompt
// append / codex 角色专属 home 的 AGENTS.md。子会话不挂 MCP——spawn
// 深度硬性为 1，防递归失控。
func (s *OrchService) roleInjection(role *model.Role, agent *model.Agent) (orchInjection, error) {
	switch flavorOfAgent(agent) {
	case "claude":
		meta := map[string]any{}
		if strings.TrimSpace(role.Persona) != "" {
			meta["systemPrompt"] = map[string]any{"append": "\n# 角色设定\n\n" + role.Persona}
		}
		// 子代理同样收口内部 Task：层级失控的另一半入口。
		meta["claudeCode"] = map[string]any{"options": map[string]any{
			"disallowedTools": []string{"Task"},
		}}
		return orchInjection{MetaExtra: meta}, nil
	case "codex":
		home := filepath.Join(s.dataDir, "orch", fmt.Sprintf("role-home-%d", role.ID))
		persona := ""
		if strings.TrimSpace(role.Persona) != "" {
			persona = "# 角色设定\n\n" + role.Persona + "\n"
		}
		if err := s.ensureOrchCodexHome(home, persona, nil); err != nil {
			return orchInjection{}, fmt.Errorf("build role codex home: %w", err)
		}
		return orchInjection{
			ExtraEnv: map[string]string{"CODEX_HOME": home},
		}, nil
	}
	return orchInjection{}, nil
}

// ensureOrchCodexHome 搭好编排用的 codex 家目录：基座（auth 软链 /
// config 复制 / 技能包软链）复用技能隔离的逻辑，之后覆盖写两样编排
// 专属内容——AGENTS.md（调度提示词或角色 persona，用户改了角色要生效，
// 每次重写）与 config.toml 尾部的 acpp MCP 段（mcpURL 非 nil 时；端口
// 随部署形态变，同样每次重写）。
func (s *OrchService) ensureOrchCodexHome(home, agentsMD string, mcpURL *string) error {
	if err := acp.EnsureCodexHome(home, s.skillpackDir, os.Getenv("HOME")); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(home, "AGENTS.md"), []byte(agentsMD), 0o644); err != nil {
		return fmt.Errorf("write AGENTS.md: %w", err)
	}

	// config.toml 每次由系统副本重建：codex 会往 config 写回运行态
	//（trust_level 等），但编排 home 的 config 必须跟随系统配置与我们的
	// 追加段，可复现性优先于保留写回。
	raw, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".codex", "config.toml"))
	if err != nil {
		raw = nil // 未装 codex 或无配置：空基座也能写我们的段。
	}
	cfg := string(raw)
	if mcpURL != nil {
		cfg += fmt.Sprintf("\n[mcp_servers.acpp]\nurl = %q\ntool_timeout_sec = %d\n",
			*mcpURL, mcpToolTimeoutMS/1000)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o600); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}
	return nil
}

// ensureOpen 幂等拉起编排主会话（懒连接，语义对齐 ChatService.Open）。
func (s *OrchService) ensureOpen(ctx context.Context, orch *model.OrchSession) error {
	key := orchKey(orch.ID)
	if _, ok := s.manager.Get(key); ok {
		return nil
	}

	var agent model.Agent
	if err := s.db.WithContext(ctx).First(&agent, orch.AgentID).Error; err != nil {
		return fmt.Errorf("load agent %d: %w", orch.AgentID, err)
	}

	roles, err := s.roles.List(ctx)
	if err != nil {
		return err
	}
	inj, err := s.mainInjection(orch, &agent, buildOrchPrompt(roles))
	if err != nil {
		return err
	}

	cwd := orch.Cwd
	if cwd == "" {
		cwd = agent.Cwd
	}

	br := s.brokerFor(key)
	sess, err := s.manager.Open(ctx, acp.OpenOptions{
		Key:     key,
		Runtime: acp.RuntimeFor(agent.Command, agent.Args, agent.Env),
		Cwd:     cwd,
		OnEvent: func(ev acp.Event) { s.handleOrchEvent(orch.ID, br, ev) },
		WireTap: func(dir string, msg json.RawMessage) { s.transcripts.Append(key, dir, msg) },
		// 编排会话同样跨重启恢复：MCP/提示词注入在 load 路径一并携带。
		ResumeACPSessionID: orch.ACPSessionID,
		ExtraEnv:           inj.ExtraEnv,
		MetaExtra:          inj.MetaExtra,
		MCPServers:         inj.MCPServers,
	})
	if err != nil {
		s.markOrchError(orch.ID, err)
		return fmt.Errorf("open orch session: %w", err)
	}

	updates := map[string]any{
		"acp_session_id": sess.ACPSessionID(),
		"cwd":            cwd,
	}
	if err := s.db.WithContext(ctx).Model(&model.OrchSession{}).
		Where("id = ?", orch.ID).Updates(updates).Error; err != nil {
		return fmt.Errorf("save orch acp session id: %w", err)
	}
	orch.ACPSessionID = sess.ACPSessionID()
	orch.Cwd = cwd

	if settings, err := s.manager.Settings(key); err == nil {
		s.saveOrchSettingsSnapshot(orch.ID, &settings)
		br.publish(StreamEvent{Kind: "settings", Settings: &settings})
	}
	if commands := s.manager.Commands(key); len(commands) > 0 {
		br.publish(StreamEvent{Kind: "commands", Commands: commands})
	}
	return nil
}
