package remote

import (
	"context"
)

// 把服务器观察工具面挂给一条会话。
//
// 挂载是**有条件**的：一台服务器都没配时什么都不挂——工具清单里凭空多出
// 七个用不了的条目只会让模型乱试，也白占它的注意力。但只要配了，就对所有
// 会话都挂：服务器不做项目隔离（adr-019），一台机器上跑多个项目是常态。
//
// 两端注入口的差异与数据库那面一致（实测见 team-mode-protocol-findings）：
// claude 走 `_meta.claudeCode.options.mcpServers` 并把工具名写进 allowedTools
// 预批——观察类工具一轮排障要调十几次，每次弹权限卡没有意义（全部只读，
// 真正的边界是 SSH 账号自身的权限）；codex 走 session/new 的 mcpServers。
//
// 挂的只有**工具**，没有提示词：什么时候该看服务器、该看哪个目录，由模型
// 从任务与项目代码自己判断，用法手册在 skill 里按需加载。开场就铺一段
// 服务器说明，等于每条会话都替用户按下「我要看线上」。
func (s *Service) MountsFor(ctx context.Context, sessionID uint, cwd, flavor string) ([]any, map[string]any, error) {
	if s.sessions == nil {
		return nil, nil, nil
	}
	list, err := s.Enabled(ctx)
	if err != nil || len(list) == 0 {
		return nil, nil, err
	}

	token, err := s.sessions.EnsureMCPToken(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	servers, meta := s.mountPayload(flavor, token)
	return servers, meta, nil
}

// MountsForPeer 为一个没有会话记录的调用方（discord 子区）算挂载。
// only 非零时可见范围锁死到那一台（频道绑定的机器）。
func (s *Service) MountsForPeer(ctx context.Context, key, cwd, flavor string, only uint) ([]any, map[string]any, error) {
	list, err := s.visible(ctx, Scope{Only: only})
	if err != nil || len(list) == 0 {
		return nil, nil, err
	}
	token, err := s.peerTok.Issue(key, cwd, only)
	if err != nil {
		return nil, nil, err
	}
	servers, meta := s.mountPayload(flavor, token)
	return servers, meta, nil
}

func (s *Service) mountPayload(flavor, token string) ([]any, map[string]any) {
	url := s.mcpBase + token

	if flavor == "claude" {
		return nil, map[string]any{
			"claudeCode": map[string]any{"options": map[string]any{
				"mcpServers": map[string]any{
					mcpServerName: map[string]any{"type": "http", "url": url},
				},
				"allowedTools": allowedTools(),
			}},
		}
	}

	servers := []any{map[string]any{
		"type":    "http",
		"name":    mcpServerName,
		"url":     url,
		"headers": []any{},
	}}
	return servers, nil
}

// allowedTools 是 claude 侧预批的工具名（`mcp__<server>__<tool>`）。
// 全部只读，逐个弹卡只会让人麻木地一路点「允许」。
func allowedTools() []string {
	names := []string{
		"server_hosts", "server_info",
		"server_ls", "server_read", "server_grep",
		"docker_ps", "docker_logs",
	}
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "mcp__" + mcpServerName + "__" + n
	}
	return out
}
