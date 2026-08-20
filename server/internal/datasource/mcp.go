package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"acpp/server/internal/mcp"
	"acpp/server/internal/model"
)

// 会话侧的数据库工具面。协议外壳复用 internal/mcp，这里只声明工具。
//
// 每个工具都先经 sourcesFor 取数据源——那一步按会话 cwd 的项目过滤，
// 所以「AI 只能看见本项目的库」不是靠工具描述里的君子协定，
// 而是它**根本拿不到**别的项目的连接。
//
// 工具名用 db_ 前缀：codex 内部有 collaboration 工具族（spawn_agent/wait/
// close_agent…），自定义工具撞上那些名字会被静默路由进内部实现（实测踩过）。

const mcpServerName = "acpp-db"

// sourceArg 是所有工具共用的数据源参数声明。
func sourceArg() map[string]any {
	return map[string]any{
		"type": "string",
		"description": "数据源，填环境名（如 dev）或 项目/环境（如 pp-game/dev）。" +
			"当前项目只有一个数据源时可省略。每个数据源固定对应一个库，不用也不能另外指定库。",
	}
}

// ServerName 是这个工具面在 agent 侧的 server 名（工具全名是
// mcp__acpp-db__db_query 这种形状）。工具台展示分组时也用它。
const ServerName = mcpServerName

// HandleMCP 处理一条发到 /api/mcp/db/{token} 的 JSON-RPC 消息。
func (s *Service) HandleMCP(ctx context.Context, token string, raw []byte) (any, bool) {
	// sessionID/cwd 由 Resolve 填好给 OnCall 用：Server 是每次请求现构造的，
	// Resolve 一定跑在 OnCall 之前，所以闭包传值比让观测端再查一次库省事。
	var sessionID uint
	var cwd string

	srv := mcp.Server{
		Name: mcpServerName,
		Resolve: func(ctx context.Context, token string) ([]mcp.Tool, error) {
			if s.sessions == nil {
				return nil, fmt.Errorf("datasource mcp not wired")
			}
			id, dir, err := s.sessions.SessionByMCPToken(ctx, token)
			if err != nil {
				return nil, err
			}
			sessionID, cwd = id, dir
			return s.toolsForCwd(ctx, dir)
		},
		OnCall: func(ctx context.Context, rec mcp.Call) {
			s.record(ctx, rec, sessionID, cwd, model.MCPSourceAgent)
		},
	}
	return srv.Serve(ctx, token, raw)
}

// InspectTools 列出一条工作目录下的工具声明，供工具台展示。
//
// 走的是与 agent 完全相同的那条 toolsForCwd：页面上看到的工具集、描述与
// 参数，就是模型此刻看到的那一份。两边各算一次的话，页面迟早会骗人。
func (s *Service) InspectTools(ctx context.Context, cwd string) ([]mcp.Declaration, error) {
	tools, err := s.toolsForCwd(ctx, cwd)
	if err != nil {
		return nil, err
	}
	return mcp.Declare(tools), nil
}

// InspectMCP 以工作目录（而非会话 token）为上下文处理一条 JSON-RPC 消息，
// 供工具台的试运行与自定义请求使用。
//
// 刻意让它走完整的协议外壳而不是直接调 Tool.Call：工具台的价值就在于
// 复现 AI 那一侧的真实往返——参数怎么解析、错误包成什么形状、返回长什么
// 样，都该和 agent 看到的一模一样。试运行只是替用户把 tools/call 的
// 请求体拼好了而已。
func (s *Service) InspectMCP(ctx context.Context, cwd string, raw []byte) (any, bool) {
	srv := mcp.Server{
		Name: mcpServerName,
		Resolve: func(ctx context.Context, _ string) ([]mcp.Tool, error) {
			return s.toolsForCwd(ctx, cwd)
		},
		OnCall: func(ctx context.Context, rec mcp.Call) {
			s.record(ctx, rec, 0, cwd, model.MCPSourceManual)
		},
	}
	return srv.Serve(ctx, "", raw)
}

// toolsForCwd 算出一条工作目录下可用的工具集。
//
// 执行工具只在**存在可写数据源**时才出现在清单里：全是只读连接的项目，
// 模型连这个工具都看不到，也就不会去试。
func (s *Service) toolsForCwd(ctx context.Context, cwd string) ([]mcp.Tool, error) {
	sources, err := s.ForCwd(ctx, cwd, true)
	if err != nil {
		return nil, err
	}
	writable := false
	for i := range sources {
		if !sources[i].ReadOnly {
			writable = true
			break
		}
	}
	return s.tools(cwd, writable), nil
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

// tools 构造这条会话可用的工具集。cwd 决定项目，项目决定数据源；
// writable 决定要不要挂执行工具。
func (s *Service) tools(cwd string, writable bool) []mcp.Tool {
	// 每个工具都现取数据源而不是闭包捕获一份：配置页刚改完的连接，
	// 下一次调用就该生效，不该等会话重开。
	sources := func(ctx context.Context) ([]model.DataSource, error) {
		return s.ForCwd(ctx, cwd, true)
	}
	pick := func(ctx context.Context, ref string) (*model.DataSource, error) {
		list, err := sources(ctx)
		if err != nil {
			return nil, err
		}
		return Resolve(list, ref)
	}

	tools := []mcp.Tool{{
		Name:        "db_sources",
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		Description: "列出当前项目可用的数据库数据源（每个环境一条：local/dev/pre…）。" +
			"不确定要连哪个环境时先调它。只能看到当前工作目录所属项目的数据源。",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Call: func(ctx context.Context, _ json.RawMessage) (string, error) {
			list, err := sources(ctx)
			if err != nil {
				return "", err
			}
			return renderSources(list), nil
		},
	}, {
		Name:        "db_tables",
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		Description: "列出数据源那个库里的表与视图（表名、引擎、估算行数、注释）。",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"source": sourceArg()},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args := parseArgs(raw)
			src, err := pick(ctx, args.Source)
			if err != nil {
				return "", err
			}
			list, err := Tables(ctx, src, "")
			if err != nil {
				return "", err
			}
			return renderTables(src, src.Database, list), nil
		},
	}, {
		Name:        "db_schema",
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		Description: "查看一张表的结构：列定义、索引与建表语句。" +
			"写 SQL 前先看结构，不要凭表名猜字段。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source": sourceArg(),
				"table":  map[string]any{"type": "string", "description": "表名"},
			},
			"required": []string{"table"},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args := parseArgs(raw)
			src, err := pick(ctx, args.Source)
			if err != nil {
				return "", err
			}
			detail, err := Describe(ctx, src, "", args.Table)
			if err != nil {
				return "", err
			}
			return renderSchema(src, detail), nil
		},
	}, {
		Name:        "db_query",
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		Description: "在数据源上查询数据（只能跑 SELECT / SHOW / DESC / EXPLAIN 一类语句，" +
			"写语句会被拒绝——改数据用 db_execute）。可一次提交多条语句（分号分隔，" +
			"按序执行、遇错即停）。结果默认最多 " + itoa(defaultMaxRows) +
			" 行——要总量用 COUNT(*)，不要靠翻页硬取。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source": sourceArg(),
				"sql":    map[string]any{"type": "string", "description": "要执行的查询，可含多条语句"},
			},
			"required": []string{"sql"},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			return runSQL(ctx, pick, raw, false)
		},
	}}

	if !writable {
		return tools
	}
	return append(tools, mcp.Tool{
		Name:        "db_execute",
		Annotations: &mcp.Annotations{DestructiveHint: true},
		Description: "在数据源上执行**会改变数据或结构**的 SQL（INSERT / UPDATE / DELETE / DDL）。" +
			"只对没有开启只读的数据源可用，只读数据源上调用会被拒绝。" +
			"可一次提交多条语句（分号分隔，按序执行、遇错即停；前面成功的不会回滚）。" +
			"跑之前先确认这是不是用户要的环境——`local` 和 `pre` 只差两个字母。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source": sourceArg(),
				"sql":    map[string]any{"type": "string", "description": "要执行的语句，可含多条"},
			},
			"required": []string{"sql"},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			return runSQL(ctx, pick, raw, true)
		},
	})
}

// runSQL 是两个执行工具的共同实现。write 为 true 时走执行通道——那时
// 数据源必须没开只读，否则连跑都不跑。
func runSQL(ctx context.Context, pick func(context.Context, string) (*model.DataSource, error),
	raw json.RawMessage, write bool) (string, error) {
	args := parseArgs(raw)
	if strings.TrimSpace(args.SQL) == "" {
		return "", fmt.Errorf("sql 不能为空")
	}
	src, err := pick(ctx, args.Source)
	if err != nil {
		return "", err
	}
	if write && src.ReadOnly {
		return "", fmt.Errorf("数据源 %s 配置为只读，不能执行写语句。"+
			"要改数据得先去数据库页把这条连接的「只读」关掉——这是用户的决定，不要绕过它",
			src.Ref)
	}
	// 库固定用连接绑定的那个：调用方指定不了，也就走不到别的库去。
	res, err := Execute(ctx, src, "", args.SQL, defaultMaxRows, write)
	if err != nil {
		return "", err
	}
	return renderExec(src, res), nil
}

type toolArgs struct {
	Source string `json:"source"`
	Table  string `json:"table"`
	SQL    string `json:"sql"`
}

// parseArgs 解析工具参数。解析失败按空参数处理——缺参数的报错由各工具
// 自己给（「没有叫 x 的数据源，可用的是…」比「参数解析失败」有用得多）。
func parseArgs(raw json.RawMessage) toolArgs {
	var args toolArgs
	_ = json.Unmarshal(raw, &args)
	return args
}
