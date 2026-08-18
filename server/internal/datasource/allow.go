package datasource

import (
	"fmt"
	"strings"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// 库级范围：一个数据源能碰哪些库。
//
// 项目隔离原本只做到数据源这一层——会话只拿得到本项目的连接。但一个
// MySQL 账号通常能连到整台实例上的全部库，于是 db_databases 会把别的
// 项目的业务库一并列出来，AI 也就看得见、查得动。范围收窄就是补这一刀。
//
// **诚实的边界**：这里做的是入口过滤——列库时过滤、指定库时校验、SQL 里
// 明写 `别的库.表` 时拒绝。它挡不住动态 SQL、存储过程、或者把库名拼进
// 字符串再执行的写法。真正的边界始终是连接账号的授权范围：要让一个项目
// 只碰得到自己的库，就给它配一个只授权那个库的 MySQL 账号。

// allowAll 是显式放开的写法。
const allowAll = "*"

// allowedDatabases 返回这个数据源允许访问的库；返回 nil 表示不限。
//
// 优先级：Databases 显式列出 > Database（配了默认库就只看那一个）> 不限。
func allowedDatabases(src *model.DataSource) []string {
	if raw := strings.TrimSpace(src.Databases); raw != "" {
		var out []string
		for _, name := range strings.Split(raw, ",") {
			name = strings.TrimSpace(name)
			if name == allowAll {
				return nil
			}
			if name != "" {
				out = append(out, name)
			}
		}
		return out
	}
	if db := strings.TrimSpace(src.Database); db != "" {
		return []string{db}
	}
	return nil
}

// databaseAllowed 判断某个库是否在范围内（库名在 MySQL 里大小写规则随
// 平台变，这里一律不区分大小写——宽一点不会造成越界，严一点会误伤）。
func databaseAllowed(src *model.DataSource, database string) bool {
	allowed := allowedDatabases(src)
	if allowed == nil {
		return true
	}
	database = strings.TrimSpace(database)
	if database == "" {
		return true // 空 = 用默认库，由连接层决定
	}
	for _, name := range allowed {
		if strings.EqualFold(name, database) {
			return true
		}
	}
	return false
}

// guardDatabase 是所有「指定了库」的入口的统一校验。
func guardDatabase(src *model.DataSource, database string) error {
	if databaseAllowed(src, database) {
		return nil
	}
	return fmt.Errorf("%w: 数据源 %s 的可访问范围不含 %q 库（当前范围：%s）",
		service.ErrForbidden, src.Ref, database,
		strings.Join(allowedDatabases(src), "、"))
}

// filterDatabases 把探查到的库清单收窄到范围内。
func filterDatabases(src *model.DataSource, list []Database) []Database {
	allowed := allowedDatabases(src)
	if allowed == nil {
		return list
	}
	out := make([]Database, 0, len(allowed))
	for _, d := range list {
		if databaseAllowed(src, d.Name) {
			out = append(out, d)
		}
	}
	return out
}

// guardStatements 拒绝明写了范围外库名的语句。
//
// 只认 `库名.` 这种限定名前缀，且在剥掉字符串与注释之后判断——数据里
// 出现的 `pp-game.users` 不该被当成跨库引用。挡不住的写法在文件头注释
// 里写清楚了：这是收窄视野，不是安全边界。
func guardStatements(src *model.DataSource, stmts []string) error {
	allowed := allowedDatabases(src)
	if allowed == nil {
		return nil
	}
	for _, stmt := range stmts {
		for _, name := range qualifiedDatabases(stmt) {
			if !databaseAllowed(src, name) {
				return fmt.Errorf("%w: 语句引用了范围外的库 %q（数据源 %s 的范围：%s）",
					service.ErrForbidden, name, src.Ref, strings.Join(allowed, "、"))
			}
		}
	}
	return nil
}

// tableKeywords 是「下一个标识符出现在表位置」的关键字。
var tableKeywords = map[string]bool{
	"from": true, "join": true, "into": true, "update": true, "table": true,
}

// qualifiedDatabases 提取语句里**表位置**上的 `<库>.` 限定名。
//
// 只看表位置是必须的：`SELECT u.id FROM app.users AS u` 里的 `u.` 是表别名
// 限定列，不是库引用。全局扫所有 `xxx.` 会把几乎每条带 JOIN 的语句都误判
// 成跨库——那种误伤比漏判更让人没法干活。
func qualifiedDatabases(stmt string) []string {
	tokens := identTokens(stripLiterals(stmt))
	var out []string
	for i := 0; i+1 < len(tokens); i++ {
		if !tableKeywords[strings.ToLower(tokens[i])] {
			continue
		}
		// 表位置上的 `db.table`：点号前那段是库名。三段式
		// `db.tbl.col` 在表位置不合法，取第一段即可。
		name, _, ok := strings.Cut(tokens[i+1], ".")
		if !ok || name == "" || (name[0] >= '0' && name[0] <= '9') {
			continue
		}
		out = append(out, name)
	}
	return out
}

// identTokens 把裸语句切成标识符 token（含点号，`a.b` 是一个 token）。
func identTokens(s string) []string {
	var out []string
	for i, n := 0, len(s); i < n; {
		if !isIdentByte(s[i]) {
			i++
			continue
		}
		start := i
		for i < n && (isIdentByte(s[i]) || s[i] == '.') {
			i++
		}
		out = append(out, s[start:i])
	}
	return out
}

// stripLiterals 去掉字符串字面量与注释，只留语法骨架。
// 库名检测必须在骨架上做：`WHERE note = 'pp-game.users'` 里的是数据不是引用。
func stripLiterals(s string) string {
	var b strings.Builder
	for i, n := 0, len(s); i < n; {
		rest := s[i:]
		switch c := s[i]; {
		case c == '\'' || c == '"':
			b.WriteByte(' ')
			i = scanQuoted(s, i)
		case c == '`':
			// 反引号是标识符引用，内容要保留（`pp-game`.users 是真跨库）。
			j := scanQuoted(s, i)
			b.WriteString(strings.ReplaceAll(s[i+1:max(j-1, i+1)], "``", "`"))
			i = j
		case c == '-' && strings.HasPrefix(rest, "--") &&
			(len(rest) == 2 || rest[2] == ' ' || rest[2] == '\t' || rest[2] == '\n' || rest[2] == '\r'):
			i = skipLine(s, i)
		case c == '#':
			i = skipLine(s, i)
		case c == '/' && strings.HasPrefix(rest, "/*"):
			b.WriteByte(' ')
			i = skipBlockComment(s, i)
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' || c == '-' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// ── 读写闸门 ───────────────────────────────────────────────────────────
//
// 数据源上的 ReadOnly 开关决定这条连接能不能改数据。MCP 侧把查询与执行
// 拆成两个工具（db_query / db_execute），能力边界因此直接体现在工具清单
// 里——只读数据源的会话里，模型根本看不到执行工具。
//
// **这是闸门不是边界**：按首关键字判断语句类型，挡不住存储过程里的写
// 操作、动态拼出来再执行的 SQL、有副作用的函数。真正的边界是账号授权。

// readOnlyStatements 是首关键字属于「只查不改」的语句。
//
// 用白名单：SQL 的写语句种类远多于读语句，黑名单漏一个就等于放行。
// CALL 刻意不在其中——存储过程里干什么，从调用点看不出来。
var readOnlyStatements = map[string]bool{
	"select": true, "show": true, "desc": true, "describe": true,
	"explain": true, "use": true, "values": true, "table": true, "help": true,
}

// writeKeywords 是 WITH 开头的语句里一旦出现就说明它会改数据的关键字
// （MySQL 8 的 `WITH ... UPDATE/DELETE`）。
var writeKeywords = []string{"insert", "update", "delete", "replace", "merge"}

// isReadOnlyStatement 判断一条语句是不是只查不改。
func isReadOnlyStatement(stmt string) bool {
	head := strings.ToLower(firstWord(stmt))
	if head == "with" {
		// WITH 可以带 SELECT，也可以带 UPDATE/DELETE——要看里面。
		bare := strings.ToLower(stripLiterals(stmt))
		for _, kw := range writeKeywords {
			if containsWord(bare, kw) {
				return false
			}
		}
		return true
	}
	return readOnlyStatements[head]
}

// containsWord 判断裸语句里是否出现了独立的关键字（不是别的词的一部分）。
func containsWord(s, word string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], word)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(word)
		if !isIdentByte(byteAt(s, start-1)) && !isIdentByte(byteAt(s, end)) {
			return true
		}
		i = end
	}
}

func byteAt(s string, i int) byte {
	if i < 0 || i >= len(s) {
		return ' '
	}
	return s[i]
}

// guardWrites 在不允许写的调用里拦下写语句。
func guardWrites(src *model.DataSource, stmts []string, allowWrite bool) error {
	if allowWrite {
		return nil
	}
	for _, stmt := range stmts {
		if isReadOnlyStatement(stmt) {
			continue
		}
		if src.ReadOnly {
			return fmt.Errorf("%w: 数据源 %s 配置为只读，不能执行写语句（%s…）。"+
				"确实要改数据的话，去数据库页把这条连接的「只读」关掉",
				service.ErrForbidden, src.Ref, firstWord(stmt))
		}
		return fmt.Errorf("%w: 这是查询通道，只能跑 SELECT/SHOW 一类语句"+
			"（收到 %s…）。要改数据用 db_execute",
			service.ErrInvalid, firstWord(stmt))
	}
	return nil
}
