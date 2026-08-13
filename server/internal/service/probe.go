package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"acpp/server/internal/acp"
	"acpp/server/internal/model"
)

// ProbeAgent 拉一个临时会话读取 agent 的统一设置能力（flavor 与模型清单），
// 缓存进 Agent 记录后立即关闭。新会话页靠这份缓存在建会话之前展示
// 跨 agent 的模型清单。探测失败时清单置空并记下错误，不影响 agent 使用。
func (s *ChatService) ProbeAgent(ctx context.Context, agentID uint) (*model.Agent, error) {
	var agent model.Agent
	if err := s.db.WithContext(ctx).First(&agent, agentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("agent %d: %w", agentID, ErrNotFound)
		}
		return nil, fmt.Errorf("load agent %d: %w", agentID, err)
	}

	// 探测会话不干活，cwd 用独立的临时目录，不碰 agent 配置的工作目录。
	probeCwd := filepath.Join(os.TempDir(), "acpp-probe")
	key := fmt.Sprintf("probe-agent-%d", agentID)

	updates := map[string]any{}
	_, err := s.manager.Open(ctx, acp.OpenOptions{
		Key:     key,
		Runtime: acp.RuntimeFor(agent.Command, agent.Args, agent.Env),
		Cwd:     probeCwd,
		OnEvent: func(acp.Event) {},
	})
	if err != nil {
		updates["flavor"] = ""
		updates["models"] = model.AgentModelSlice{}
		updates["commands"] = model.AgentCommandSlice{}
		updates["skeleton"] = model.AgentSkeleton{}
		updates["status"] = model.AgentError
		updates["last_error"] = truncateError(err.Error())
	} else {
		settings, serr := s.manager.Settings(key)
		// 斜杠命令清单以通知形式在会话建立后异步推到，等它一小会——
		// 拿不到就算了（agent 可能真的没有命令），不拖垮探测。
		var commands []acp.Command
		for range 30 {
			if commands = s.manager.Commands(key); len(commands) > 0 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		_ = s.manager.Close(key)
		if serr != nil {
			return nil, serr
		}
		// 重探不清空配置页的取舍：禁用标记与用户起的别名都保留。
		oldDisabled := map[string]bool{}
		oldAlias := map[string]string{}
		for _, m := range agent.Models {
			if m.Disabled {
				oldDisabled["m:"+m.ID] = true
			}
			if m.Alias != "" {
				oldAlias[m.ID] = m.Alias
			}
		}
		for _, c := range agent.Commands {
			if c.Disabled {
				oldDisabled["c:"+c.Name] = true
			}
		}
		models := make(model.AgentModelSlice, 0, len(settings.Models))
		for _, m := range settings.Models {
			alias := oldAlias[m.ID]
			if alias == "" {
				// 模型 id 往往比 runtime 展示名短得多（"default" vs
				// "Default (recommended)"），自动派生成别名；派生结果
				// 反而更长时保留原名不写。
				if d := deriveModelAlias(m.ID); d != "" && len(d) < len(m.Name) {
					alias = d
				}
			}
			models = append(models, model.AgentModel{
				ID: m.ID, Name: m.Name, Description: m.Description,
				Disabled: oldDisabled["m:"+m.ID],
				Alias:    alias,
			})
		}
		cached := make(model.AgentCommandSlice, 0, len(commands))
		for _, c := range commands {
			cached = append(cached, model.AgentCommand{
				Name: c.Name, Description: c.Description,
				Disabled: oldDisabled["c:"+c.Name],
			})
		}
		skeleton := model.AgentSkeleton{
			PlanSupported: settings.PlanSupported,
			FastSupported: settings.FastSupported,
		}
		for _, e := range settings.Efforts {
			skeleton.Efforts = append(skeleton.Efforts, string(e))
		}
		for _, l := range settings.Levels {
			skeleton.Levels = append(skeleton.Levels, string(l))
		}
		updates["flavor"] = string(settings.Flavor)
		updates["models"] = models
		updates["commands"] = cached
		updates["skeleton"] = skeleton
		updates["status"] = model.AgentIdle
		updates["last_error"] = ""
		// 快速模式取舍首次落默认：claude 的 fast 额外计费默认关，其余默认开。
		// 用户改过（非空）就不再动。
		if agent.FastPolicy == "" {
			if settings.Flavor == acp.FlavorClaude {
				updates["fast_policy"] = "off"
			} else {
				updates["fast_policy"] = "on"
			}
		}
	}

	if err := s.db.WithContext(ctx).Model(&model.Agent{}).Where("id = ?", agentID).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("save agent probe result: %w", err)
	}
	if err := s.db.WithContext(ctx).First(&agent, agentID).Error; err != nil {
		return nil, fmt.Errorf("reload agent %d: %w", agentID, err)
	}
	return &agent, nil
}

// deriveModelAlias 从模型 id 派生短别名：去掉方括号后缀、首字母大写。
func deriveModelAlias(id string) string {
	name := id
	if i := strings.Index(name, "["); i > 0 {
		name = name[:i]
	}
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// ProbeAgentAsync 在后台探测（注册/更新 agent 后自动触发），完成后结果落库。
func (s *ChatService) ProbeAgentAsync(agentID uint) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if _, err := s.ProbeAgent(ctx, agentID); err != nil {
			slog.Warn("probe agent", "agent", agentID, "err", err)
		}
	}()
}
