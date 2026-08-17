package orch

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
	"acpp/server/internal/stream"
)

// flavorOfAgent 判断 runtime 方言：优先探测缓存，缺失时按命令名兜底。
// 注入口选择（systemPrompt vs CODEX_HOME）依赖它。
func flavorOfAgent(agent *model.Agent) string {
	if agent.Flavor != "" {
		return agent.Flavor
	}
	return string(acp.FlavorOf(agent.Name, agent.Command))
}

// buildOrchPrompt 组装主会话的调度提示词：spawn_agent 用法 + 角色雇佣
// 目录。写给 AI 看——「默认委派」是编排模式的核心：用户不会点名工具，
// 主控必须自己判断把实质性工作派给谁；task 必须自包含是子代理没有
// 上下文的直接推论。
func buildOrchPrompt(roles []model.Role) string {
	var b strings.Builder
	b.WriteString("\n# 编排模式\n\n")
	b.WriteString("你是编排主控（协调者）。你的默认工作方式是**委派**：" +
		"实质性工作交给角色子代理完成，你负责拆解任务、选择角色、把关结果、向用户汇总。" +
		"通过 acpp 提供的 MCP 工具 `hire_role(role, task)` 雇佣子代理。\n\n")
	b.WriteString("## 可用角色（role 参数填名字）\n\n")
	if len(roles) == 0 {
		b.WriteString("（当前没有可用角色，hire_role 不可用——自己完成任务。）\n")
	}
	for _, r := range roles {
		fmt.Fprintf(&b, "- **%s**：%s\n", r.Name, r.Description)
	}
	b.WriteString(`
## 按规模选模式（委派不是免费的）

每次委派都有固定成本：子代理看不到你的上下文，必须重新勘察现场，一个子任务动辄消耗六位数 token 与数分钟等待。**用与任务规模匹配的最小模式**，用户不会点名角色或工具，模式由你判断：

- **直接干（0 个子代理）**：问答、解释、几分钟能完成的小事（改几行、调一处文案、跑个命令看结果）。你自己有完整的读写与执行能力，别为琐事开会。
- **单角色（1 个子任务）**：边界清晰的单一职责工作（写一个模块、修一个明确的 bug、审一次改动）。派一个最匹配的角色，不配前置调研、不配收尾复验——除非产出可疑。
- **流水线（2–4 个子任务，按需串行）**：真正的多阶段工作（需求实现：调研 → 实现 → 审查 → 验证），每一步的产出喂给下一步的 task。阶段能省则省：现场已经清楚就不派调研，改动很小就不派独立验证。
- **多视角并行（fan-out）**：只留给大范围、高价值的调研/审计（真实规模的代码库、要做决策依据的报告）。同一问题按视角拆开并行（正确性/安全/性能/边界，同一角色可雇多份），返回后交叉去重汇总。**小项目不配 fan-out**。

相互独立的子任务并行派发（同一条消息里发多个 hire_role 调用）；拿不准规模时先自己快速看一眼现场，再决定模式——一次 ls/读文件比一个子代理便宜四个数量级。

## 委派规则

- task 必须自包含：写清背景、目标、涉及路径与验收标准——子代理看不到你的对话上下文。
- hire_role 会同步等待子代理完成并返回结论；长任务会先返回「任务 #N 仍在后台运行」的提示——此时**立即调用 wait_task(task_id=N) 接力等待**，循环直到拿到结论，绝不要因此重新派发或放弃。
- 子代理彼此独立、不共享记忆，跨子任务的协调与结果整合由你负责。
- 调用失败会返回错误文本：可以重试、换角色或自己处理，不要静默放弃任务。
- **只用 acpp 的 hire_role 委派**：不要使用运行时内置的 spawn_agent/collaboration 等内部多代理工具——那些子代理对用户不可见，违背编排的目的。
- 子代理的结论要经你判断后再采信：结果可疑就换个角色复核或自己抽查。
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
//   - codex：MCP 同样走 session/new 传入——config.toml 定义的 http MCP
//     是懒连接（启动不 tools/list，模型工具清单里看不到 spawn_agent，
//     实测主控会直接编造结果而不调用）；session/new 注入的会立即连接
//     列出。专属 CODEX_HOME 只承载 AGENTS.md 调度提示词与隔离。
func (s *Service) mainInjection(orch *model.OrchSession, agent *model.Agent, prompt string) (orchInjection, error) {
	mcpURL := s.mcpBase + orch.MCPToken
	mcpServers := []any{map[string]any{
		"type":    "http",
		"name":    "acpp",
		"url":     mcpURL,
		"headers": []any{},
	}}
	switch flavorOfAgent(agent) {
	case "claude":
		return orchInjection{
			ExtraEnv: map[string]string{
				"MCP_TOOL_TIMEOUT": strconv.Itoa(mcpToolTimeoutMS),
			},
			MetaExtra: map[string]any{
				"systemPrompt": map[string]any{"append": prompt},
				"claudeCode": map[string]any{"options": map[string]any{
					// 收内部 Task 子代理，预批我们自己的派发工具——
					// 每次 spawn 都弹权限卡没有意义（事件层另有自动放行兜底）。
					"disallowedTools": []string{"Task"},
					"allowedTools":    []string{"mcp__acpp__hire_role", "mcp__acpp__wait_task"},
					// acpp server 走 SDK options 注入而不是 session/new 的
					// mcpServers：只有这条路能带 per-server timeout——
					// MCP_TOOL_TIMEOUT env 存在一层 5 分钟 clamp（实测
					// 318s/339s 超时），长任务全靠这里的 timeout 突破。
					"mcpServers": map[string]any{
						"acpp": map[string]any{
							"type":    "http",
							"url":     mcpURL,
							"timeout": mcpToolTimeoutMS,
						},
					},
				}},
			},
		}, nil
	case "codex":
		home := filepath.Join(s.dataDir, "orch", fmt.Sprintf("home-%d", orch.ID))
		// config.toml 不写 acpp 段：与 session/new 注入的同名 server
		// 冲突会让会话建不起来。
		if err := s.ensureOrchCodexHome(home, prompt, nil); err != nil {
			return orchInjection{}, fmt.Errorf("build orch codex home: %w", err)
		}
		return orchInjection{
			ExtraEnv:   map[string]string{"CODEX_HOME": home},
			MCPServers: mcpServers,
		}, nil
	}
	// generic runtime 没有可靠的注入口：不挂 MCP、不注提示词，
	// 编排会话退化成普通对话（创建时已挡，这里兜底）。
	return orchInjection{}, nil
}

// roleInjection 组装角色子会话注入：persona 进 claude 的 systemPrompt
// append / codex 角色专属 home 的 AGENTS.md。子会话不挂 MCP——spawn
// 深度硬性为 1，防递归失控。
func (s *Service) roleInjection(role *model.Role, agent *model.Agent) (orchInjection, error) {
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
func (s *Service) ensureOrchCodexHome(home, agentsMD string, mcpURL *string) error {
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
func (s *Service) ensureOpen(ctx context.Context, orch *model.OrchSession) error {
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
		OnEvent: func(ev acp.Event) { s.handleOrchEvent(orch.ID, key, br, ev) },
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
		br.Publish(stream.Event{Kind: "settings", Settings: &settings})
	}
	if commands := s.manager.Commands(key); len(commands) > 0 {
		br.Publish(stream.Event{Kind: "commands", Commands: commands})
	}
	return nil
}
