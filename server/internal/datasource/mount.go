package datasource

import (
	"context"
	"strings"
)

// 把数据库工具面挂给一条普通会话。
//
// 挂载是**有条件**的：会话所在项目没有可用数据源时什么都不挂——工具清单
// 里凭空多出五个用不了的 db_* 只会让模型乱试，也白占它的注意力。
//
// 两端注入口不同（实测，见 team-mode-protocol-findings）：
//   - claude：MCP 走 `_meta.claudeCode.options.mcpServers`，同时把工具名
//     写进 allowedTools 预批——数据库工具一次会话里要调好几轮，每轮都弹
//     权限卡没有意义（用户已经拍板：能跑什么由数据库账号决定）。
//   - codex：走 session/new 的 mcpServers。不能写进 config.toml——那里
//     定义的 http MCP 是懒连接，启动不 tools/list，模型工具清单里看不到，
//     实测它宁可编答案也不调用。
//
// 挂的只有**工具**，没有提示词：数据源清单与用法说明不进开场上下文，等用户
// 真的 @ 引用了数据库再随引用一起给（service.AppendDBReferences）。开场就
// 铺一段数据库说明，等于每条会话都替用户按下「我要动数据库」——用户拍板：
// 不主动提，用到才说。

// MountsFor 为一条会话算出要挂载的 MCP server 与 _meta 追加内容。
// 没有可用数据源时返回三个零值，调用方原样跳过。
func (s *Service) MountsFor(ctx context.Context, sessionID uint, cwd, flavor string) ([]any, map[string]any, error) {
	if s.sessions == nil || strings.TrimSpace(cwd) == "" {
		return nil, nil, nil
	}
	sources, err := s.ForCwd(ctx, cwd, true)
	if err != nil || len(sources) == 0 {
		return nil, nil, err
	}

	token, err := s.sessions.EnsureMCPToken(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	url := s.mcpBase + token

	if flavor == "claude" {
		return nil, map[string]any{
			"claudeCode": map[string]any{"options": map[string]any{
				"mcpServers": map[string]any{
					mcpServerName: map[string]any{"type": "http", "url": url},
				},
				"allowedTools": allowedTools(),
			}},
		}, nil
	}

	servers := []any{map[string]any{
		"type":    "http",
		"name":    mcpServerName,
		"url":     url,
		"headers": []any{},
	}}
	return servers, nil, nil
}

// allowedTools 是 claude 侧预批的工具名（`mcp__<server>__<tool>`）。
func allowedTools() []string {
	names := []string{"db_sources", "db_databases", "db_tables", "db_schema", "db_query"}
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "mcp__" + mcpServerName + "__" + n
	}
	return out
}
