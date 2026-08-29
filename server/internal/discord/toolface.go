package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"acpp/server/internal/mcp"
	"acpp/server/internal/webshot"
)

// acpp-chat 工具面：agent 主动把文件**发给用户**的出口。网页会话里成果
// 摊开靠预览面板；discord 的等价物是「东西直接出现在频道里」——图片原样
// 发，HTML 先渲染成整页长图（与报告长图同一条 webshot 通道），其余文件
// 当附件。工具面由本包自己声明与执行（凭证/路径护栏/发送都在这），协议
// 外壳复用 internal/mcp。

const chatServerName = "acpp-chat"
const chatToolSend = "send_file"

// chatMounts 为一个子区会话算自家工具面的挂载载荷。
func (s *Service) chatMounts(threadID string, b Binding) (servers []any, meta map[string]any, err error) {
	if s.deps.MCPBase == "" {
		return nil, nil, nil
	}
	token, err := s.chatTok.Issue(threadID, b.Workdir)
	if err != nil {
		return nil, nil, err
	}
	url := strings.TrimRight(s.deps.MCPBase, "/") + "/" + token
	if b.Agent == "claude" {
		return nil, map[string]any{
			"claudeCode": map[string]any{"options": map[string]any{
				"mcpServers": map[string]any{
					chatServerName: map[string]any{"type": "http", "url": url},
				},
				"allowedTools": []string{"mcp__" + chatServerName + "__" + chatToolSend},
			}},
		}, nil
	}
	return []any{map[string]any{
		"type": "http", "name": chatServerName, "url": url, "headers": []any{},
	}}, nil, nil
}

// HandleChatMCP 处理一条发到 /api/mcp/discord/{token} 的 JSON-RPC 消息。
func (s *Service) HandleChatMCP(ctx context.Context, token string, raw []byte) (any, bool) {
	srv := mcp.Server{
		Name: chatServerName,
		Resolve: func(ctx context.Context, token string) ([]mcp.Tool, error) {
			threadID, cwd, ok := s.chatTok.Lookup(token)
			if !ok {
				return nil, fmt.Errorf("凭证无效（会话可能已重启）")
			}
			return s.chatTools(threadID, cwd), nil
		},
	}
	return srv.Serve(ctx, token, raw)
}

const sendFileDescription = "把工作目录里的一个文件直接发到当前 Discord 对话里给用户看。" +
	"什么时候用：你生成了图片、图表、HTML 页面或其他文件，用户需要**看到**它——" +
	"贴路径没有用，用户不在你的机器上。.html 会自动渲染成整页长图发出（报告优先走 " +
	"report_open，其他 HTML 才用这里）；图片原样发；其余文件当附件（上限 24MB）。" +
	"path 相对工作目录；caption 可选，一句话说明这是什么。"

// chatTools 构造子区会话可用的工具集。
func (s *Service) chatTools(threadID, cwd string) []mcp.Tool {
	return []mcp.Tool{{
		Name:        chatToolSend,
		Description: sendFileDescription,
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "文件路径，相对工作目录"},
				"caption": map[string]any{"type": "string", "description": "可选，随文件显示的一句话说明"},
			},
			"required": []any{"path"},
		},
		Call: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Path    string `json:"path"`
				Caption string `json:"caption"`
			}
			if len(args) > 0 {
				if err := json.Unmarshal(args, &in); err != nil {
					return "", fmt.Errorf("参数解析失败：%w", err)
				}
			}
			return s.sendFileToThread(ctx, threadID, cwd, in.Path, in.Caption)
		},
	}}
}

// sendFileToThread 执行一次 send_file：护栏 → 按类型转换 → 发送。
func (s *Service) sendFileToThread(ctx context.Context, threadID, cwd, rel, caption string) (string, error) {
	abs, err := resolveInWorkdir(cwd, rel)
	if err != nil {
		return "", err
	}
	token := s.store.config().BotToken
	name := filepath.Base(abs)
	content := "-# 📎 " + trimRunes(orDefault(caption, name), 150)

	var data []byte
	filename := name
	switch {
	case strings.EqualFold(filepath.Ext(abs), ".html"):
		// HTML 渲染成整页长图——原文件浏览器都没有的用户打不开。
		cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		data, err = webshot.Capture(cctx, "file://"+abs, 1100)
		if err != nil {
			return "", fmt.Errorf("HTML 渲染失败（本机需要 Chrome）：%w", err)
		}
		filename = strings.TrimSuffix(name, filepath.Ext(name)) + ".png"
		content = "-# 🖼 " + trimRunes(orDefault(caption, name), 150)
	default:
		data, err = os.ReadFile(abs)
		if err != nil {
			return "", fmt.Errorf("读文件：%w", err)
		}
	}
	if len(data) > reportImageMax {
		return "", fmt.Errorf("文件 %d MB，超过发送上限 24MB", len(data)>>20)
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	err = botRESTFile(cctx, token, threadID, map[string]any{
		"content":          content,
		"attachments":      []map[string]any{{"id": 0, "filename": filename}},
		"allowed_mentions": noMentions(),
	}, filename, data)
	if err != nil {
		return "", fmt.Errorf("发送失败：%w", err)
	}
	return fmt.Sprintf("已把 %s 发进对话，用户现在能看到它了。", filename), nil
}

// resolveInWorkdir 把路径解析成确认落在工作目录内的绝对路径（软链先
// 解析再比对，护栏口径与 ReportPath 一致）。
func resolveInWorkdir(workdir, rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("path 不能为空")
	}
	abs := rel
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(workdir, rel)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("文件不存在：%s", rel)
	}
	root, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		return "", fmt.Errorf("解析工作目录: %w", err)
	}
	if resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return "", fmt.Errorf("路径在工作目录之外")
	}
	if info, err := os.Stat(resolved); err != nil || info.IsDir() {
		return "", fmt.Errorf("不是一个文件：%s", rel)
	}
	return resolved, nil
}
