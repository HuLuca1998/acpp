package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"acpp/server/internal/mcp"
	"acpp/server/internal/model"
)

// 会话侧的服务器观察工具面。协议外壳复用 internal/mcp，这里只声明工具。
//
// 与数据库那一面最大的不同：**不做项目隔离**（adr-019）。数据源按 cwd
// 推项目过滤，服务器不过滤——一台机器上跑多个项目是常态，按项目切会把
// AI 需要的上下文一起切掉。可见范围只有一个收窄口：discord 频道锁定的
// 那台（Scope.Only）。
//
// 工具描述是这套东西唯一的「使用说明」——挂载时不注入任何提示词（照
// datasource 的口径），模型什么时候用、怎么用，全靠这些描述与 skill。
// 所以描述里写的是**判断依据**（什么场景该用它、代价多大、怎么收窄），
// 不是参数的复述。

const mcpServerName = "acpp-server"

// ServerName 是这个工具面在 agent 侧的 server 名（工具全名形如
// mcp__acpp-server__server_read）。工具台展示分组时也用它。
const ServerName = mcpServerName

// Scope 是一次调用能看见哪些服务器。
//
// 零值表示「全部启用的服务器」——这是常态。Only 非零时锁死到那一台
// （discord 频道绑定的机器），连别的机器都列不出来。
type Scope struct {
	Only uint
}

// serverArg 是所有工具共用的服务器参数声明。
func serverArg() map[string]any {
	return map[string]any{
		"type": "string",
		"description": "服务器名，取自 server_hosts。" +
			"只配了一台时可省略。",
	}
}

// HandleMCP 处理一条发到 /api/mcp/server/{token} 的 JSON-RPC 消息。
func (s *Service) HandleMCP(ctx context.Context, token string, raw []byte) (any, bool) {
	var sessionID uint
	var cwd string

	srv := mcp.Server{
		Name: mcpServerName,
		Resolve: func(ctx context.Context, token string) ([]mcp.Tool, error) {
			// 非会话凭证（discord 子区）优先：不落库，sessionID 记 0。
			// 凭证上带着作用域——频道锁定了哪台机器，这枚凭证就只看得见哪台。
			if _, dir, only, ok := s.peerTok.Lookup(token); ok {
				cwd = dir
				return s.tools(Scope{Only: only}), nil
			}
			if s.sessions == nil {
				return nil, fmt.Errorf("remote mcp not wired")
			}
			id, dir, err := s.sessions.SessionByMCPToken(ctx, token)
			if err != nil {
				return nil, err
			}
			sessionID, cwd = id, dir
			return s.tools(Scope{}), nil
		},
		OnCall: func(ctx context.Context, rec mcp.Call) {
			s.record(ctx, rec, sessionID, cwd, model.MCPSourceAgent)
		},
	}
	return srv.Serve(ctx, token, raw)
}

// InspectTools 列出工具声明，供工具台展示。走的是与 agent 完全相同的
// 那份 tools()，页面上看到的就是模型此刻看到的。
func (s *Service) InspectTools() []mcp.Declaration {
	return mcp.Declare(s.tools(Scope{}))
}

// InspectMCP 处理工具台的试运行与自定义请求，走完整协议外壳。
func (s *Service) InspectMCP(ctx context.Context, cwd string, raw []byte) (any, bool) {
	srv := mcp.Server{
		Name: mcpServerName,
		Resolve: func(context.Context, string) ([]mcp.Tool, error) {
			return s.tools(Scope{}), nil
		},
		OnCall: func(ctx context.Context, rec mcp.Call) {
			s.record(ctx, rec, 0, cwd, model.MCPSourceManual)
		},
	}
	return srv.Serve(ctx, "", raw)
}

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

// visible 返回这个作用域能看见的服务器。
func (s *Service) visible(ctx context.Context, sc Scope) ([]model.Server, error) {
	list, err := s.Enabled(ctx)
	if err != nil {
		return nil, err
	}
	if sc.Only == 0 {
		return list, nil
	}
	// 锁定到一台时查不到就返回空——降级方向只能是「更少」，
	// 绝不能悄悄回退成整台机器都可见。
	for i := range list {
		if list[i].ID == sc.Only {
			return list[i : i+1], nil
		}
	}
	return nil, nil
}

// pick 按名字挑一台服务器。只配了一台时允许省略名字——那时没有歧义，
// 让模型少写一个参数比让它多猜一次好。
func (s *Service) pick(ctx context.Context, sc Scope, name string) (*model.Server, error) {
	list, err := s.visible(ctx, sc)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("还没有配置任何服务器（在「服务器」页里加一台）")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		if len(list) == 1 {
			return &list[0], nil
		}
		return nil, fmt.Errorf("有 %d 台服务器，要指定 server：%s",
			len(list), strings.Join(namesOf(list), "、"))
	}
	for i := range list {
		if strings.EqualFold(list[i].Name, name) {
			return &list[i], nil
		}
	}
	return nil, fmt.Errorf("没有叫 %s 的服务器，可用的是：%s",
		name, strings.Join(namesOf(list), "、"))
}

func namesOf(list []model.Server) []string {
	out := make([]string, len(list))
	for i := range list {
		out[i] = list[i].Name
	}
	return out
}

// tools 构造工具集。全部只读，所以不像数据库那面还要按可写与否分档。
func (s *Service) tools(sc Scope) []mcp.Tool {
	pick := func(ctx context.Context, name string) (*model.Server, error) {
		return s.pick(ctx, sc, name)
	}
	var tools []mcp.Tool
	tools = append(tools, s.hostTools(sc, pick)...)
	tools = append(tools, s.fileTools(pick)...)
	tools = append(tools, s.dockerTools(pick)...)
	return tools
}

// picker 是各工具文件共用的「按名字取服务器」签名。
type picker func(ctx context.Context, name string) (*model.Server, error)

// toolArgs 是全部工具的参数并集。合成一个结构是因为工具很多而参数高度
// 重合，各写一个类型只会让「这个参数在哪几个工具里有」更难看清。
type toolArgs struct {
	Server string `json:"server"`
	Path   string `json:"path"`
	// 文件读写
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
	Tail   int `json:"tail"`
	// 列目录
	Depth   int    `json:"depth"`
	Pattern string `json:"pattern"`
	Sort    string `json:"sort"`
	// 搜索
	IgnoreCase bool   `json:"ignoreCase"`
	Context    int    `json:"context"`
	Include    string `json:"include"`
	MaxMatches int    `json:"maxMatches"`
	// docker
	Container  string `json:"container"`
	All        bool   `json:"all"`
	Filter     string `json:"filter"`
	Since      string `json:"since"`
	Until      string `json:"until"`
	Grep       string `json:"grep"`
	Timestamps bool   `json:"timestamps"`
}

// parseArgs 解析工具参数。解析失败按空参数处理——缺参数的报错由各工具
// 自己给（「没有叫 x 的服务器，可用的是…」比「参数解析失败」有用得多）。
func parseArgs(raw json.RawMessage) toolArgs {
	var args toolArgs
	_ = json.Unmarshal(raw, &args)
	return args
}

// clamp 把一个数值夹进 [1, max]，0 或负数取默认值。
func clamp(v, def, max int) int {
	if v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}
