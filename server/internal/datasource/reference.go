package datasource

import (
	"context"
	"fmt"
	"strings"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// @ 数据库引用：用户在输入框里挑一个数据源/库/表，发送时把它的现状嵌进
// prompt——与 @ 文件引用同一个动作，只是内容从磁盘换成了库。
//
// 为什么值得嵌而不是让 AI 自己去查：一次引用省掉一到两轮工具调用，
// 更要紧的是它**指定了范围**。「帮我看下这个表」比「你自己找找」少一半
// 误会，尤其是有多个环境的时候。
//
// 引用串的形状与 MCP 工具的 source 参数保持一致：
//
//	pp-game/dev              数据源（带默认库的表清单）
//	pp-game/dev/mydb         指定库（表清单）
//	pp-game/dev/mydb/users   指定表（列、索引、建表语句）

// refMaxTables 是库级引用最多列出的表数。库大起来表能上千，
// 全列进去就把上下文吃光了——超出部分只报数量，AI 要细节自己调工具。
const refMaxTables = 200

// Reference 把引用串展开成可嵌进 prompt 的文本。
//
// cwd 决定可见哪些数据源——与 MCP 工具面同一个过滤入口，引用不是绕过
// 项目边界的后门。展开失败（数据源不存在、连不上、表没了）直接报错：
// 发出去一个空引用比报错更糟，用户会以为 AI 看到了其实没看到的东西。
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
		ref, err := expandRef(ctx, sources, raw)
		if err != nil {
			return nil, err
		}
		out = append(out, *ref)
	}
	return out, nil
}

func expandRef(ctx context.Context, sources []model.DataSource, raw string) (*service.DBReference, error) {
	src, database, table, err := splitRef(sources, raw)
	if err != nil {
		return nil, err
	}

	uri := "mysql://" + src.Ref
	if database != "" {
		uri += "/" + database
	}
	if table != "" {
		uri += "/" + table
	}

	// 语气很要紧：引用表达的是「接下来的问题针对这个库，你去查它」，
	// 不是「这是一份数据快照」。只给结构不点破的话，模型常拿着结构就
	// 开始推测数据，答出一堆看着合理的数字——用户要的是真去查。
	var b strings.Builder
	b.WriteString("# 本轮指定的数据库\n\n")
	fmt.Fprintf(&b, "用户引用了数据源 **%s**（%s:%d", src.Ref, src.Host, src.Port)
	if src.Note != "" {
		fmt.Fprintf(&b, "，%s", src.Note)
	}
	b.WriteString("）")
	if database != "" {
		fmt.Fprintf(&b, "的 `%s` 库", database)
	}
	if table != "" {
		fmt.Fprintf(&b, "的 `%s` 表", table)
	}
	b.WriteString("。\n\n")
	fmt.Fprintf(&b, "**接下来凡是与数据有关的问题，都用 acpp-db 的 db_* 工具实际查询它**"+
		"（`source` 参数填 `%s`），不要凭下面的结构推测数据、也不要问用户该连哪个库——"+
		"他已经指定了。下面的结构信息是给你省掉一次 db_schema，不是数据本身。\n\n",
		src.Ref)

	switch {
	case table != "":
		detail, err := Describe(ctx, src, database, table)
		if err != nil {
			return nil, err
		}
		b.WriteString(renderSchema(src, detail))

	case database != "":
		tables, err := Tables(ctx, src, database)
		if err != nil {
			return nil, err
		}
		b.WriteString(renderRefTables(src, database, tables))

	default:
		// 数据源级引用：没有默认库就只给数据源信息，让 AI 自己 db_databases。
		if strings.TrimSpace(src.Database) == "" {
			b.WriteString("（该数据源未配默认库，用 db_databases 查看有哪些库。）\n")
			break
		}
		tables, err := Tables(ctx, src, src.Database)
		if err != nil {
			return nil, err
		}
		b.WriteString(renderRefTables(src, src.Database, tables))
	}

	return &service.DBReference{URI: uri, Text: b.String()}, nil
}

// splitRef 把引用串拆成数据源 + 库 + 表。
//
// 数据源部分是 `<项目>/<环境>` 两段（项目与环境都不含斜杠，建时已挡），
// 所以按斜杠切开后前两段归数据源，第三段是库、第四段是表。只写一段的
// 也认——那是只给环境名的写法，与 MCP 工具的 source 参数一致。
func splitRef(sources []model.DataSource, raw string) (*model.DataSource, string, string, error) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(raw), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return nil, "", "", fmt.Errorf("%w: 空的数据库引用", service.ErrInvalid)
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
		return nil, "", "", err
	}

	var database, table string
	if len(rest) > 0 {
		database = rest[0]
	}
	if len(rest) > 1 {
		table = rest[1]
	}
	return src, database, table, nil
}

// renderRefTables 是库级引用的正文：表清单 + 用法提示。
func renderRefTables(src *model.DataSource, database string, tables []Table) string {
	var b strings.Builder
	if len(tables) == 0 {
		fmt.Fprintf(&b, "%s 的 %s 库里没有表。\n", src.Ref, database)
		return b.String()
	}

	shown := tables
	if len(shown) > refMaxTables {
		shown = shown[:refMaxTables]
	}
	fmt.Fprintf(&b, "%s 的 %s 库共 %d 张表", src.Ref, database, len(tables))
	if len(shown) < len(tables) {
		fmt.Fprintf(&b, "（只列前 %d 张）", len(shown))
	}
	b.WriteString("：\n")

	rows := [][]string{{"表", "类型", "行数(估算)", "注释"}}
	for _, t := range shown {
		rows = append(rows, []string{
			t.Name, shortTableType(t.Type), fmt.Sprint(t.Rows), dash(t.Comment),
		})
	}
	b.WriteString(table(rows))
	b.WriteString("\n要看某张表的字段用 db_schema，要数据用 db_query——写 SQL 前先看结构，别凭表名猜字段。\n")
	return b.String()
}
