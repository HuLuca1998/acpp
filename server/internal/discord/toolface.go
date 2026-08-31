package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"acpp/server/internal/mcp"
	"acpp/server/internal/webshot"
)

// acpp-chat 工具面：agent 主动把文件**发给用户**的出口。网页会话里成果
// 摊开靠预览面板；discord 的等价物是「文件直接出现在频道里」——一律以
// 附件上传，用户能点开、能下载、能转发。
//
// .html 发两个附件：原文件 + 整页长图（与报告长图同一条 webshot 通道）。
// 早先只发长图，用户点名要过原文件——图能看不能存，说「把 xxx.html 发
// 上来」的人要的是那个文件本身。渲染失败也照发原文件，降级不空手。
//
// 工具面由本包自己声明与执行（凭证/路径护栏/发送都在这），协议外壳复用
// internal/mcp。

const chatServerName = "acpp-chat"
const chatToolSend = "send_file"

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
			threadID, cwd, _, ok := s.chatTok.Lookup(token)
			if !ok {
				return nil, fmt.Errorf("凭证无效（会话可能已重启）")
			}
			return s.chatTools(threadID, cwd), nil
		},
	}
	return srv.Serve(ctx, token, raw)
}

const sendFileDescription = "把工作目录里的文件直接发到当前 Discord 对话里给用户看——" +
	"**文件本体会作为附件上传**，用户在手机上就能点开、下载、转发。" +
	"什么时候用：用户开口要某个文件（「把 xxx 发上来」「给我那份报告」「日志发我看看」）；" +
	"或者你产出了图片、图表、HTML 页面、数据文件，用户需要拿到它——" +
	"贴路径没有用，用户不在你的机器上，读一遍文件内容也不等于给了他文件。" +
	"paths 可以一次传多个（相对工作目录，最多 10 个）。" +
	".html 默认发**两个附件**：原文件 + 整页长图（长图给手机上直接看，原文件给存档和浏览器打开）；" +
	"用户明确说只要文件本身就传 preview=false，省掉渲染那一步。" +
	"报告类成果优先走 report_open（它也是原文件 + 长图一起发），其他文件才用这里。" +
	"caption 可选，一句话说明这是什么。"

// chatTools 构造子区会话可用的工具集。
func (s *Service) chatTools(threadID, cwd string) []mcp.Tool {
	return []mcp.Tool{{
		Name:        chatToolSend,
		Description: sendFileDescription,
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"paths": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "文件路径，相对工作目录；一次最多 10 个",
				},
				"caption": map[string]any{"type": "string", "description": "可选，随文件显示的一句话说明"},
				"preview": map[string]any{
					"type": "boolean",
					"description": "可选，默认 true。.html 是否附带整页长图；" +
						"用户明确只要文件本身时传 false（原文件照发，不受影响）",
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
				Caption string `json:"caption"`
				Preview *bool  `json:"preview"`
			}
			if len(args) > 0 {
				if err := json.Unmarshal(args, &in); err != nil {
					return "", fmt.Errorf("参数解析失败：%w", err)
				}
			}
			paths := in.Paths
			if strings.TrimSpace(in.Path) != "" {
				paths = append(paths, in.Path)
			}
			preview := in.Preview == nil || *in.Preview
			return s.sendFilesToThread(ctx, threadID, cwd, paths, in.Caption, preview)
		},
	}}
}

// sendMaxFiles 是一次调用能发的文件数上限（Discord 单条消息 10 个附件，
// 一个 .html 会占两个，所以按文件数收在 10 以内、发送时再分批）。
const sendMaxFiles = 10

// sendFilesToThread 执行一次 send_file：逐个护栏 + 转换，然后分批发进子区。
// 单个文件失败不拖累别的——发得出去的先发出去，失败的在回执里说清楚，
// 模型据此决定要不要换个路径重试。
func (s *Service) sendFilesToThread(ctx context.Context, threadID, cwd string, rels []string, caption string, preview bool) (string, error) {
	if len(rels) == 0 {
		return "", fmt.Errorf("paths 不能为空")
	}
	if len(rels) > sendMaxFiles {
		return "", fmt.Errorf("一次最多发 %d 个文件，这次传了 %d 个", sendMaxFiles, len(rels))
	}

	var files []outFile
	var sent, failed []string
	for _, rel := range rels {
		out, err := prepareOutFiles(ctx, cwd, rel, preview)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s（%s）", rel, err.Error()))
			continue
		}
		files = append(files, out...)
		sent = append(sent, filepath.Base(rel))
	}
	if len(files) == 0 {
		return "", fmt.Errorf("一个都没发出去：%s", strings.Join(failed, "；"))
	}

	content := "-# 📎 " + trimRunes(orDefault(caption, strings.Join(sent, "、")), 150)
	token := s.store.config().BotToken
	if err := s.postAttachments(ctx, token, threadID, content, files); err != nil {
		return "", fmt.Errorf("发送失败：%w", err)
	}

	msg := fmt.Sprintf("已把 %s 作为附件发进对话，用户现在能点开和下载了。", strings.Join(sent, "、"))
	if len(failed) > 0 {
		msg += " 没发出去的：" + strings.Join(failed, "；")
	}
	return msg, nil
}

// prepareOutFiles 把一个路径转成待发附件。.html 出两个（原文件 + 长图），
// 其余原样一个。长图渲染失败**不算失败**——原文件照发，用户至少拿到了
// 他要的那个文件。
func prepareOutFiles(ctx context.Context, cwd, rel string, preview bool) ([]outFile, error) {
	abs, err := resolveInWorkdir(cwd, rel)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("读文件失败")
	}
	if len(data) > sendAttachMax {
		return nil, fmt.Errorf("%d MB，超过上限 24MB", len(data)>>20)
	}
	name := filepath.Base(abs)
	out := []outFile{{name: name, data: data}}
	if !preview || !strings.EqualFold(filepath.Ext(abs), ".html") {
		return out, nil
	}

	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	png, err := webshot.Capture(cctx, "file://"+abs, 1100)
	if err != nil {
		slog.Warn("send_file：HTML 渲染失败，只发原文件", "path", rel, "err", err)
		return out, nil
	}
	if len(png)+len(data) > sendAttachMax {
		slog.Warn("send_file：长图放不下，只发原文件", "path", rel, "bytes", len(png))
		return out, nil
	}
	// 长图排前面：Discord 把第一个附件渲染成预览大图，图在前才有得看。
	return []outFile{{name: strings.TrimSuffix(name, filepath.Ext(name)) + ".png", data: png}, out[0]}, nil
}

// sendAttachMax 与报告附件同一口径（Discord 免费档 25MB，留余量）。
const sendAttachMax = reportAttachMax

// maxAttachPerMsg 是 Discord 单条消息的附件个数上限。
const maxAttachPerMsg = 10

// batchFiles 按 Discord 单条消息的上限（10 个附件、合计 25MB）把附件切
// 成若干批。单个文件超限在上游已经拦掉，这里只管怎么装箱。
func batchFiles(files []outFile) [][]outFile {
	var batches [][]outFile
	var cur []outFile
	size := 0
	for _, f := range files {
		if len(cur) == maxAttachPerMsg || (len(cur) > 0 && size+len(f.data) > sendAttachMax) {
			batches = append(batches, cur)
			cur, size = nil, 0
		}
		cur = append(cur, f)
		size += len(f.data)
	}
	if len(cur) > 0 {
		batches = append(batches, cur)
	}
	return batches
}

// postAttachments 把一批附件发进子区，超出单条上限的分多条发。分多条时
// 只有第一条带说明文字，后面几条是纯附件——同一句说明重复三遍比不说更吵。
func (s *Service) postAttachments(ctx context.Context, token, channelID, content string, files []outFile) error {
	for i, batch := range batchFiles(files) {
		payload := map[string]any{"allowed_mentions": noMentions()}
		if i == 0 && content != "" {
			payload["content"] = content
		}
		cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		err := botRESTFiles(cctx, token, channelID, payload, batch)
		cancel()
		if err != nil {
			return err
		}
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
