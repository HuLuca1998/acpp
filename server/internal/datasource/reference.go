package datasource

import (
	"context"
	"fmt"
	"strings"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// @ 数据库引用：用户在输入框里挑一个数据源/库/表，发送时随 prompt 告知
// AI「这轮针对这个库，用 acpp-db 的工具去查它」——与 @ 文件引用同一个动作。
//
// 引用的价值是**指定范围**，不是搬运数据：早先这里会现查库把表清单/表结构
// 嵌进 prompt，几千 token 的常驻负担换来的信息 AI 自己一次 db_tables/db_schema
// 就能拿到（用户拍板：引用只给指路的告知，结构让 AI 用工具现查）。
//
// 引用串两级——一条连接固定对应一个库，中间没有「选库」这一层：
//
//	pp-game/dev         数据源（它绑定的那个库）
//	pp-game/dev/users   指定表

// Reference 把引用串展开成可嵌进 prompt 的告知文本。
//
// cwd 决定可见哪些数据源——与 MCP 工具面同一个过滤入口，引用不是绕过
// 项目边界的后门。数据源不存在直接报错：发出去一个空引用比报错更糟，
// 用户会以为 AI 看到了其实没看到的东西。这里不连库——引用不该因为库
// 一时连不上而发不出消息，连不连得上等 AI 真正调工具时再暴露。
func (s *Service) Reference(ctx context.Context, cwd string, refs []string) ([]service.DBReference, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	sources, err := s.ForCwd(ctx, cwd, true)
	if err != nil {
		return nil, err
	}

	out := make([]service.DBReference, 0, len(refs))
	for _, raw := range refs {
		ref, err := expandRef(sources, raw)
		if err != nil {
			return nil, err
		}
		out = append(out, *ref)
	}
	return out, nil
}

func expandRef(sources []model.DataSource, raw string) (*service.DBReference, error) {
	src, table, err := splitRef(sources, raw)
	if err != nil {
		return nil, err
	}

	uri := "mysql://" + src.Ref + "/" + src.Database
	if table != "" {
		uri += "/" + table
	}

	// 语气很要紧：引用表达的是「接下来的问题针对这个库，你去查它」，
	// 不是「这是一份数据快照」。不点破的话，模型常凭表名开始推测数据，
	// 答出一堆看着合理的数字——用户要的是真去查。
	var b strings.Builder
	b.WriteString("# 本轮指定的数据库\n\n")
	fmt.Fprintf(&b, "用户引用了数据源 **%s**（%s:%d", src.Ref, src.Host, src.Port)
	if src.Note != "" {
		fmt.Fprintf(&b, "，%s", src.Note)
	}
	fmt.Fprintf(&b, "）的 `%s` 库", src.Database)
	if table != "" {
		fmt.Fprintf(&b, "的 `%s` 表", table)
	}
	b.WriteString("。这条连接只对应这一个库。\n\n")
	fmt.Fprintf(&b, "接下来凡是与数据有关的问题，都用 acpp-db 的 db_* 工具实际查询它"+
		"（`source` 参数填 `%s`）：先用 db_tables / db_schema 看表清单与表结构再写 SQL，"+
		"不要凭表名猜字段；要改数据前先确认环境——环境名往往只差一两个字母，跑错了不可撤销。"+
		"不要凭空推测数据，也不要问用户该连哪个库——他已经指定了。\n", src.Ref)

	return &service.DBReference{URI: uri, Text: b.String()}, nil
}

// splitRef 把引用串拆成数据源 + 表。
//
// 数据源部分是 `<项目>/<环境>` 两段（项目与环境都不含斜杠，建时已挡），
// 按斜杠切开后前两段归数据源，剩下的是表名。只写一段的也认——那是只给
// 环境名的写法，与 MCP 工具的 source 参数一致。
func splitRef(sources []model.DataSource, raw string) (*model.DataSource, string, error) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(raw), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return nil, "", fmt.Errorf("%w: 空的数据库引用", service.ErrInvalid)
	}

	// 先按两段找，找不到再按一段找（只给了环境名）。
	var (
		src  *model.DataSource
		rest []string
		err  error
	)
	if len(parts) >= 2 {
		src, err = Resolve(sources, parts[0]+"/"+parts[1])
		rest = parts[2:]
	}
	if src == nil {
		src, err = Resolve(sources, parts[0])
		rest = parts[1:]
	}
	if err != nil || src == nil {
		if err == nil {
			err = fmt.Errorf("%w: 没有叫 %q 的数据源", service.ErrNotFound, raw)
		}
		return nil, "", err
	}

	var table string
	if len(rest) > 0 {
		// 兼容旧写法 `<项目>/<环境>/<库>/<表>`：库那一段与连接绑定的库
		// 相同就跳过，剩下的当表名。
		if len(rest) > 1 && strings.EqualFold(rest[0], src.Database) {
			rest = rest[1:]
		}
		table = rest[0]
	}
	return src, table, nil
}
