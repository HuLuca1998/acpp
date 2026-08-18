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
			"当前项目只有一个数据源时可省略。",
	}
}

func databaseArg() map[string]any {
	return map[string]any{
		"type":        "string",
		"description": "数据库名。省略则用数据源配置的默认库。",
	}
}

// HandleMCP 处理一条发到 /api/mcp/db/{token} 的 JSON-RPC 消息。
func (s *Service) HandleMCP(ctx context.Context, token string, raw []byte) (any, bool) {
	return mcp.Serve(ctx, mcpServerName, token, raw, func(ctx context.Context, token string) ([]mcp.Tool, error) {
		if s.sessions == nil {
			return nil, fmt.Errorf("datasource mcp not wired")
		}
		cwd, err := s.sessions.CwdByMCPToken(ctx, token)
		if err != nil {
			return nil, err
		}
		// 执行工具只在**存在可写数据源**时才出现在清单里：全是只读连接
		// 的会话，模型连这个工具都看不到，也就不会去试。
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
		Name: "db_sources",
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
		Name: "db_databases",
		Description: "列出这个数据源允许访问的数据库（库名、字符集、表数量）。" +
			"数据源可能限定了范围，列出来的就是全部可用的库——不在其中的库访问会被拒绝。",
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
			list, err := Databases(ctx, src)
			if err != nil {
				return "", err
			}
			return renderDatabases(src, list), nil
		},
	}, {
		Name:        "db_tables",
		Description: "列出一个数据库里的表与视图（表名、引擎、估算行数、注释）。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source":   sourceArg(),
				"database": databaseArg(),
			},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args := parseArgs(raw)
			src, err := pick(ctx, args.Source)
			if err != nil {
				return "", err
			}
			list, err := Tables(ctx, src, args.Database)
			if err != nil {
				return "", err
			}
			return renderTables(src, firstNonEmpty(args.Database, src.Database), list), nil
		},
	}, {
		Name: "db_schema",
		Description: "查看一张表的结构：列定义、索引与建表语句。" +
			"写 SQL 前先看结构，不要凭表名猜字段。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source":   sourceArg(),
				"database": databaseArg(),
				"table":    map[string]any{"type": "string", "description": "表名"},
			},
			"required": []string{"table"},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args := parseArgs(raw)
			src, err := pick(ctx, args.Source)
			if err != nil {
				return "", err
			}
			detail, err := Describe(ctx, src, args.Database, args.Table)
			if err != nil {
				return "", err
			}
			return renderSchema(src, detail), nil
		},
	}, {
		Name: "db_query",
		Description: "在数据源上查询数据（只能跑 SELECT / SHOW / DESC / EXPLAIN 一类语句，" +
			"写语句会被拒绝——改数据用 db_execute）。可一次提交多条语句（分号分隔，" +
			"按序执行、遇错即停）。结果默认最多 " + itoa(defaultMaxRows) +
			" 行——要总量用 COUNT(*)，不要靠翻页硬取。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source":   sourceArg(),
				"database": databaseArg(),
				"sql":      map[string]any{"type": "string", "description": "要执行的查询，可含多条语句"},
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
		Name: "db_execute",
		Description: "在数据源上执行**会改变数据或结构**的 SQL（INSERT / UPDATE / DELETE / DDL）。" +
			"只对没有开启只读的数据源可用，只读数据源上调用会被拒绝。" +
			"可一次提交多条语句（分号分隔，按序执行、遇错即停；前面成功的不会回滚）。" +
			"跑之前先确认这是不是用户要的环境——`local` 和 `pre` 只差两个字母。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source":   sourceArg(),
				"database": databaseArg(),
				"sql":      map[string]any{"type": "string", "description": "要执行的语句，可含多条"},
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
	res, err := Execute(ctx, src, args.Database, args.SQL, defaultMaxRows, write)
	if err != nil {
		return "", err
	}
	return renderExec(src, res), nil
}

type toolArgs struct {
	Source   string `json:"source"`
	Database string `json:"database"`
	Table    string `json:"table"`
	SQL      string `json:"sql"`
}

// parseArgs 解析工具参数。解析失败按空参数处理——缺参数的报错由各工具
// 自己给（「没有叫 x 的数据源，可用的是…」比「参数解析失败」有用得多）。
func parseArgs(raw json.RawMessage) toolArgs {
	var args toolArgs
	_ = json.Unmarshal(raw, &args)
	return args
}
