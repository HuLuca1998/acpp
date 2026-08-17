package datasource

import (
	"fmt"
	"strconv"
	"strings"

	"acpp/server/internal/model"
)

// 工具结果的文本渲染。MCP 的 content 是纯文本，模型读到的就是这些字符串。
//
// 三条取舍：
//   - **表格而不是 JSON**：同样的信息，列名不必每行重复一遍，token 只有
//     JSON 的一半左右。
//   - **制表符分隔而不是空格对齐**：对模型一样好读，但少了大量填充空格；
//     更要紧的是它**可解析**——前端要把 AI 的这次查询渲染成真正的表格
//     组件（列头 + 可滚动数据），靠的就是这个稳定格式。单元格里的制表符
//     与换行在 oneLine 里已经清掉，分隔符不会被数据撞上。
//   - **每段都带数据源与库名**：模型在多环境间来回切时，光看结果就知道
//     这是哪个库的数据。
//
// 改这里的格式要同步改前端的 lib/db-result.ts（两端共用这一份约定）。

// maxCellWidth 是单元格显示宽度上限。长文本字段（json、正文）一行就能
// 顶掉几百 token，截断后模型仍看得出有内容，要全文再单独查那一行。
const maxCellWidth = 120

func renderSources(sources []model.DataSource) string {
	if len(sources) == 0 {
		return "当前项目没有配置数据源。（数据源按项目隔离，需要在面板的「数据库」页里为本项目添加连接。）"
	}
	rows := [][]string{{"数据源", "环境", "地址", "默认库", "SSH", "备注"}}
	for _, s := range sources {
		ssh := "-"
		if s.SSHEnabled {
			ssh = s.SSHUser + "@" + s.SSHHost
		}
		rows = append(rows, []string{
			s.Ref, s.Env, fmt.Sprintf("%s:%d", s.Host, s.Port),
			dash(s.Database), ssh, dash(s.Note),
		})
	}
	return fmt.Sprintf("当前项目可用数据源 %d 个：\n%s", len(sources), table(rows))
}

func renderDatabases(src *model.DataSource, list []Database) string {
	if len(list) == 0 {
		return fmt.Sprintf("%s 上没有可见的数据库（可能是账号权限限制）。", src.Ref)
	}
	rows := [][]string{{"数据库", "表数", "字符集", "类型"}}
	for _, d := range list {
		kind := "业务库"
		if d.System {
			kind = "系统库"
		}
		rows = append(rows, []string{d.Name, itoa(d.Tables), dash(d.Charset), kind})
	}
	return fmt.Sprintf("数据源 %s（%s:%d）上的数据库 %d 个：\n%s",
		src.Ref, src.Host, src.Port, len(list), table(rows))
}

func renderTables(src *model.DataSource, database string, list []Table) string {
	if len(list) == 0 {
		return fmt.Sprintf("%s 的 %s 库里没有表。", src.Ref, database)
	}
	rows := [][]string{{"表", "类型", "引擎", "行数(估算)", "注释"}}
	for _, t := range list {
		rows = append(rows, []string{
			t.Name, shortTableType(t.Type), dash(t.Engine),
			strconv.FormatInt(t.Rows, 10), dash(t.Comment),
		})
	}
	return fmt.Sprintf("%s 的 %s 库共 %d 张表：\n%s",
		src.Ref, database, len(list), table(rows))
}

func renderSchema(src *model.DataSource, d *TableDetail) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s.%s\n\n", src.Ref, d.Database, d.Name)

	rows := [][]string{{"列", "类型", "可空", "键", "默认值", "额外", "注释"}}
	for _, c := range d.Columns {
		rows = append(rows, []string{
			c.Name, c.Type, yesNo(c.Nullable), dash(c.Key),
			dash(c.Default), dash(c.Extra), dash(c.Comment),
		})
	}
	b.WriteString(table(rows))

	if len(d.Indexes) > 0 {
		b.WriteString("\n索引：\n")
		for _, idx := range d.Indexes {
			kind := "INDEX"
			if idx.Unique {
				kind = "UNIQUE"
			}
			fmt.Fprintf(&b, "  %s %s (%s)\n", kind, idx.Name, strings.Join(idx.Columns, ", "))
		}
	}
	if d.DDL != "" {
		fmt.Fprintf(&b, "\n建表语句：\n%s\n", d.DDL)
	}
	return b.String()
}

func renderExec(src *model.DataSource, res *ExecResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s · %d 条语句 · %dms\n",
		src.Ref, dash(res.Database), len(res.Results), res.ElapsedMS)

	for i, r := range res.Results {
		fmt.Fprintf(&b, "\n[%d] %s\n", i+1, oneLine(r.Statement))
		switch {
		case r.Error != "":
			// 失败要写清「后面的没跑」，否则模型会以为整段都执行了。
			fmt.Fprintf(&b, "失败：%s\n", r.Error)
			if remaining := len(res.Results) - i - 1; remaining > 0 {
				fmt.Fprintf(&b, "（其后 %d 条语句未执行）\n", remaining)
			}
		case r.Kind == "query":
			if r.RowCount == 0 {
				b.WriteString("0 行\n")
				continue
			}
			rows := [][]string{r.Columns}
			for _, row := range r.Rows {
				cells := make([]string, len(row))
				for j, v := range row {
					cells[j] = cell(v)
				}
				rows = append(rows, cells)
			}
			fmt.Fprintf(&b, "%d 行 · %dms\n%s", r.RowCount, r.ElapsedMS, table(rows))
			if r.Truncated {
				fmt.Fprintf(&b, "（只显示前 %d 行，结果被截断——要总量请用 COUNT(*)）\n", r.RowCount)
			}
		default:
			fmt.Fprintf(&b, "影响 %d 行 · %dms", r.Affected, r.ElapsedMS)
			if r.LastInsertID > 0 {
				fmt.Fprintf(&b, " · 自增 id %d", r.LastInsertID)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// table 画一张制表符分隔的表，首行是表头。
func table(rows [][]string) string {
	var b strings.Builder
	for _, row := range rows {
		b.WriteString(strings.Join(row, "\t"))
		b.WriteString("\n")
	}
	return b.String()
}

func cell(v any) string {
	if v == nil {
		return "NULL"
	}
	s := oneLine(fmt.Sprint(v))
	if len(s) > maxCellWidth {
		return s[:maxCellWidth] + "…"
	}
	return s
}

// oneLine 把多行内容压成一行：表格里换行会把列对齐全部打乱。
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(strings.ReplaceAll(s, "\t", " "))
}

func shortTableType(t string) string {
	if strings.EqualFold(t, "BASE TABLE") {
		return "表"
	}
	if strings.EqualFold(t, "VIEW") {
		return "视图"
	}
	return t
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func yesNo(b bool) string {
	if b {
		return "是"
	}
	return "否"
}

func itoa(n int) string { return strconv.Itoa(n) }
