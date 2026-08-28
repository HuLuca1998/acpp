package report

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"acpp/server/internal/mcp"
	"acpp/server/internal/model"
)

// 工具名用 report_ 前缀：codex 内部有 collaboration 工具族
// （spawn_agent/wait/close_agent…），自定义工具撞上那些名字会被静默路由
// 进内部实现（datasource 实测踩过）。裸 open / preview 这类通用词风险最大。
const mcpServerName = "acpp-report"

// ServerName 是这个工具面在 agent 侧的 server 名（工具全名形如
// mcp__acpp-report__report_open）。工具台展示分组时也用它。
const ServerName = mcpServerName

const toolOpen = "report_open"

// openDescription 是这个能力**唯一的触发器**，写给模型看。
//
// 它与 html-report 技能是一对：技能管「报告写成什么样」，工具管「什么
// 时候摊开给人看」，两边互相点名。只有技能没有工具，模型写完文件不知道
// 要打开，用户什么也看不见；只有工具没有技能，打开的是一堵套着 HTML 皮
// 的文字墙。
//
// 工具比技能更容易被想起来：技能的描述躺在「可用命令」清单里，模型得
// 主动回想；而工具清单是它每一步决策都会扫的东西。所以这段描述要把
// 「产出成果 → 写成 html → 打开」这条链子说完整。
const openDescription = "把一份已经写好的单文件 HTML 报告在用户的工作区里打开给他看。" +
	"什么时候用：当你产出的是有结构的成果——项目/模块介绍、调研、排查复盘、" +
	"周报、多方案对比、架构与数据流讲解、实施计划——用线性文字讲会把结构压扁，" +
	"就按 html-report 技能的规范写成一个单文件 .html，然后**必须调用这个工具**把它打开；" +
	"不调用的话文件只是躺在磁盘上，用户根本看不到。" +
	"path 传报告文件的路径（相对会话工作目录即可），只能是工作目录内的 .html。" +
	"不要把 HTML 内容当参数传进来——它只收路径，你用自己的写文件工具产出内容，" +
	"要改就 Edit 那个文件再调一次同一路径，视图会更新。"

// HandleMCP 处理一条发到 /api/mcp/report/{token} 的 JSON-RPC 消息。
func (s *Service) HandleMCP(ctx context.Context, token string, raw []byte) (any, bool) {
	// sessionID/cwd 由 Resolve 填好给 OnCall 用：Server 是每次请求现构造
	// 的，Resolve 一定跑在 OnCall 之前，所以闭包传值比让观测端再查一次库省事。
	var sessionID uint
	var cwd string

	srv := mcp.Server{
		Name: mcpServerName,
		Resolve: func(ctx context.Context, token string) ([]mcp.Tool, error) {
			// 非会话凭证（discord 子区）优先：打开事件走挂载时登记的
			// 回调，观测记录 sessionID 记 0（非会话发起语义）。
			if key, dir, ok := s.peerTok.Lookup(token); ok {
				cwd = dir
				return s.tools(dir, func(rel, title string) {
					if f, ok := s.peerOpen.Load(key); ok && f != nil {
						f.(func(rel, title string))(rel, title)
					}
				}), nil
			}
			if s.sessions == nil {
				return nil, fmt.Errorf("report mcp not wired")
			}
			id, dir, err := s.sessions.SessionByMCPToken(ctx, token)
			if err != nil {
				return nil, err
			}
			sessionID, cwd = id, dir
			return s.tools(dir, func(rel, title string) {
				if s.notifier != nil {
					s.notifier.ReportOpened(id, rel, title)
				}
			}), nil
		},
		OnCall: func(ctx context.Context, rec mcp.Call) {
			s.record(ctx, rec, sessionID, cwd, model.MCPSourceAgent)
		},
	}
	return srv.Serve(ctx, token, raw)
}

// InspectTools 列出工具声明，供工具台展示。走的是与 agent 完全相同的那条
// tools，页面上看到的就是模型此刻看到的那一份。
func (s *Service) InspectTools(cwd string) []mcp.Declaration {
	return mcp.Declare(s.tools(cwd, nil))
}

// InspectMCP 以工作目录（而非会话 token）为上下文处理一条 JSON-RPC 消息，
// 供工具台的试运行使用。
func (s *Service) InspectMCP(ctx context.Context, cwd string, raw []byte) (any, bool) {
	srv := mcp.Server{
		Name: mcpServerName,
		Resolve: func(ctx context.Context, _ string) ([]mcp.Tool, error) {
			// 工具台的试运行不广播：它是 owner 在管理面里手动发的请求，
			// 不该弹开某条会话的面板。
			return s.tools(cwd, nil), nil
		},
		OnCall: func(ctx context.Context, rec mcp.Call) {
			s.record(ctx, rec, 0, cwd, model.MCPSourceManual)
		},
	}
	return srv.Serve(ctx, "", raw)
}

// openArgs 是 report_open 的入参。
type openArgs struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

func (s *Service) tools(cwd string, onOpen func(rel, title string)) []mcp.Tool {
	return []mcp.Tool{{
		Name:        toolOpen,
		Description: openDescription,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type": "string",
					"description": "报告文件路径，相对会话工作目录（如 " +
						"docs/项目报告.html）。必须是工作目录内已经存在的 .html 文件。",
				},
				"title": map[string]any{
					"type":        "string",
					"description": "可选。视图标签上显示的标题；不传就用 HTML 里的 <title>。",
				},
			},
			"required": []any{"path"},
		},
		// 只读注解：这个工具不改任何文件，它只是把已有的文件摊开给人看。
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		Call: func(ctx context.Context, args json.RawMessage) (string, error) {
			var in openArgs
			if len(args) > 0 {
				if err := json.Unmarshal(args, &in); err != nil {
					return "", fmt.Errorf("参数解析失败：%w", err)
				}
			}
			abs, err := resolveInCwd(cwd, in.Path)
			if err != nil {
				return "", err
			}
			rel := relTo(cwd, abs)

			// 回给模型的这句话要说清「已经发生了什么」：它据此决定还要不要
			// 在正文里重复报告内容——不需要，用户已经在看了。
			title := strings.TrimSpace(in.Title)
			if title == "" {
				title = filepath.Base(rel)
			}
			if onOpen != nil {
				onOpen(rel, title)
			}
			return fmt.Sprintf(
				"已在用户的工作区打开报告《%s》（%s）。用户现在正看着它，"+
					"不用再把报告内容复述一遍——回一两句说明这份报告涵盖了什么就够了。",
				title, rel), nil
		},
	}}
}

// record 把一次工具调用交给观测端。没挂观测就什么都不做。
func (s *Service) record(ctx context.Context, rec mcp.Call, sessionID uint, cwd, source string) {
	if s.calls == nil {
		return
	}
	s.calls.Record(ctx, model.MCPCall{
		Server:     rec.Server,
		Tool:       rec.Tool,
		SessionID:  sessionID,
		Source:     source,
		Cwd:        cwd,
		Args:       string(rec.Args),
		Result:     rec.Result,
		IsError:    rec.IsError,
		DurationMs: rec.Duration.Milliseconds(),
	})
}
