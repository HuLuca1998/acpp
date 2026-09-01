package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"acpp/server/internal/mcp"
)

// acpp-chat 工具面：agent 把成果**交到用户手上**的出口，外加交出去之后
// 的收回权。网页会话里成果摊开靠预览面板；discord 的等价物是「东西直接
// 出现在频道里」——要么是附件，要么是一条点开即看的链接。
//
// 三个工具是一套：send_file 交付，list_links 看还有什么挂在外面，
// revoke_link 收回。只给第一个的话，外链就成了发出去再也收不回的东西
// （用户点名要过这条：「我看完之后告知 ai 删除链接」）。
//
// 工具面由本包自己声明与执行（凭证/路径护栏/发送都在这），协议外壳复用
// internal/mcp，交付形态的实现在 deliver.go。

const chatServerName = "acpp-chat"

const (
	chatToolSend   = "send_file"
	chatToolLinks  = "list_links"
	chatToolRevoke = "revoke_link"
)

// chatMounts 为一个子区会话算自家工具面的挂载载荷。
func (s *Service) chatMounts(threadID string, b Binding) (servers []any, meta map[string]any, err error) {
	if s.deps.MCPBase == "" {
		return nil, nil, nil
	}
	token, err := s.chatTok.Issue(threadID, b.Workdir, 0)
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
				"allowedTools": chatAllowedTools(),
			}},
		}, nil
	}
	return []any{map[string]any{
		"type": "http", "name": chatServerName, "url": url, "headers": []any{},
	}}, nil, nil
}

// chatAllowedTools 是 claude 侧预批的工具名。交付是「把东西给用户」的最后
// 一步，在这一步弹权限卡，用户在点之前什么都拿不到。
func chatAllowedTools() []string {
	var out []string
	for _, t := range []string{chatToolSend, chatToolLinks, chatToolRevoke} {
		out = append(out, "mcp__"+chatServerName+"__"+t)
	}
	return out
}

// HandleChatMCP 处理一条发到 /api/mcp/discord/{token} 的 JSON-RPC 消息。
func (s *Service) HandleChatMCP(ctx context.Context, token string, raw []byte) (any, bool) {
	srv := mcp.Server{
		Name: chatServerName,
		Resolve: func(ctx context.Context, token string) ([]mcp.Tool, error) {
			threadID, cwd, _, ok := s.chatTok.Lookup(token)
			if !ok {
				return nil, fmt.Errorf("凭证无效（会话可能已重启）")
			}
			return s.chatTools(threadID, cwd), nil
		},
	}
	return srv.Serve(ctx, token, raw)
}

// sendDescription 是交付能力的唯一触发器，写给模型看。它要回答的不是
// 「这个工具怎么调」，而是「什么时候该想起它、该选哪种形态」——用户开口
// 要东西时，模型的第一反应往往是把文件内容读一遍贴进对话。
const sendDescription = "把工作目录里的文件交到用户手上（当前 Discord 对话）。" +
	"用户不在你的机器上：贴路径他点不开，把内容读一遍粘出来也不等于给了他文件。" +
	"什么时候用：用户开口要东西（「把 xxx 发上来」「那份报告给我」「日志发我看看」），" +
	"或你产出了报告、图表、图片、数据文件需要交付。" +
	"\n形态由 as 决定：" +
	"\n- **用户点名了某个文件（「把 tmp/日报.html 发上来」这种带路径或文件名的），一律 as=file**：" +
	"他指名要的是那份文件本身——能存档、能转发、能再打开的那一个，不是它的展示形态。" +
	"这一条优先于下面所有默认" +
	"\n- auto（默认）：你自己产出、用户没点名具体文件时用。.html 发成**渲染后的外链**" +
	"（手机上点开就是排好版的页面），其余直接发文件" +
	"\n- file：作为附件上传原文件。用户说「文件」「原件」「别发链接」时也用这个" +
	"\n- link：发成外链（.md 与代码给 gist 页面，那里本来就有渲染与高亮）" +
	"\n- image：整页长图。用户要「截图」，或内容不便上外网时用" +
	"\n外链是不公开列出的 GitHub secret gist，但**拿到链接的人都能打开**：" +
	"涉密内容（凭据、个人信息、未公开数据）一律用 as=file，别图省事。" +
	"expire 定有效期（默认 7d，可写 12h / 30d / never）。" +
	"paths 一次最多 10 个，相对工作目录；caption 是随件的一句话说明。"

const linksDescription = "列出本对话还挂在外面的链接（发过 as=link 的那些）：标题、id、地址、什么时候到期。" +
	"用户问「我那个链接还在吗」「都发过哪些链接」，或你要撤销却手上没有 id 时用。" +
	"**要点：链接卡上有「立即失效」按钮，用户可以自己点，那不经过你**——" +
	"所以在说某条链接「还有效」之前先用这个工具核一遍，别凭对话记忆下结论。"

const revokeDescription = "撤销外链：删掉对应的 gist，外网立刻打不开。" +
	"用户说「看完了」「可以删了」「把链接撤了」时用——链接默认挂 7 天，他说不用了就别让它继续挂着。" +
	"link 传发布时给你的 id（整条链接地址也认），或传 all 撤销本对话的全部外链。" +
	"只能撤 acpp 自己发的链接，用户账号里别的 gist 碰不到。"

// chatTools 构造子区会话可用的工具集。
func (s *Service) chatTools(threadID, cwd string) []mcp.Tool {
	return []mcp.Tool{{
		Name:        chatToolSend,
		Description: sendDescription,
		// 只读注解：交付不改工作目录里的任何文件。
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"paths": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "文件路径，相对工作目录；一次最多 10 个",
				},
				"as": map[string]any{
					"type": "string",
					"enum": []any{deliverAuto, deliverFile, deliverLink, deliverImage},
					"description": "交付形态，默认 auto（.html 走外链、其余走文件）。" +
						"用户明说要文件就传 file，要截图传 image",
				},
				"caption": map[string]any{"type": "string", "description": "可选，随件的一句话说明"},
				"expire": map[string]any{
					"type":        "string",
					"description": "可选，外链有效期，默认 7d；可写 12h / 30d / never。只对外链生效",
				},
			},
			"required": []any{"paths"},
		},
		Call: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Paths []string `json:"paths"`
				// Path 是单数写法的兼容口：模型照着「一个文件」的直觉传
				// path 的概率不低，为此报错纯属自找麻烦。
				Path    string `json:"path"`
				As      string `json:"as"`
				Caption string `json:"caption"`
				Expire  string `json:"expire"`
			}
			if err := decodeArgs(args, &in); err != nil {
				return "", err
			}
			paths := in.Paths
			if strings.TrimSpace(in.Path) != "" {
				paths = append(paths, in.Path)
			}
			return s.deliverFiles(ctx, threadID, cwd, paths, in.Caption, in.As, in.Expire)
		},
	}, {
		Name:        chatToolLinks,
		Description: linksDescription,
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Call: func(ctx context.Context, _ json.RawMessage) (string, error) {
			return s.listLinks(ctx, threadID)
		},
	}, {
		Name:        chatToolRevoke,
		Description: revokeDescription,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"link": map[string]any{
					"type":        "string",
					"description": "外链 id（整条链接地址也认），或 all 撤销本对话全部外链",
				},
			},
			"required": []any{"link"},
		},
		Call: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in struct {
				Link string `json:"link"`
				// ID 是同义写法的兼容口，理由同 send_file 的 path。
				ID string `json:"id"`
			}
			if err := decodeArgs(args, &in); err != nil {
				return "", err
			}
			return s.revokeLinks(ctx, threadID, orDefault(in.Link, in.ID))
		},
	}}
}

func decodeArgs(args json.RawMessage, out any) error {
	if len(args) == 0 {
		return nil
	}
	if err := json.Unmarshal(args, out); err != nil {
		return fmt.Errorf("参数解析失败：%w", err)
	}
	return nil
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
