package report

import (
	"context"
	"strings"
)

// MountsFor 为一条会话算出要挂载的 MCP server 与 _meta 追加内容。
//
// 与数据库工具面不同，报告工具是**无条件挂**的：任何一条会话都可能产出
// 需要摊开给人看的成果，没有「这个项目没配数据源」那种前置条件。唯一的
// 前提是会话得有工作目录——路径护栏以它为准，没有它这个工具无处可校验。
//
// 两端注入口不同（沿用 datasource 实测的结论，见 team-mode-protocol-findings）：
//   - claude：MCP 走 `_meta.claudeCode.options.mcpServers`，同时把工具名
//     写进 allowedTools 预批。预批在这里比数据库那边更要紧——出一份报告
//     是「产出成果的最后一步」，在这一步弹权限卡，用户点之前什么都看不到，
//     而这个工具本身只读（它连文件都不改，只是把已有文件摊开）。
//   - codex：走 session/new 的 mcpServers。不能写进 config.toml——那里
//     定义的 http MCP 是懒连接，启动不 tools/list，模型工具清单里看不到。
//
// 挂的只有**工具**，没有提示词：不往开场上下文里塞「记得出报告」。什么
// 时候该出报告由工具自己的 description 与 html-report 技能负责，开场就
// 铺一段说明等于每条会话都替用户按下「我要看报告」。
func (s *Service) MountsFor(ctx context.Context, sessionID uint, cwd, flavor string) ([]any, map[string]any, error) {
	if s.sessions == nil || strings.TrimSpace(cwd) == "" {
		return nil, nil, nil
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
	return []string{"mcp__" + mcpServerName + "__" + toolOpen}
}
