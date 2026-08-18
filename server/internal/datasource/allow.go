package datasource

import (
	"fmt"
	"regexp"
	"strings"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// 库绑定：一条连接只对应一个库。
//
// 项目隔离原本只做到数据源这一层——会话只拿得到本项目的连接。但一个
// MySQL 账号通常能连到整台实例上的全部库，于是列库时别的项目的业务库
// 一并冒出来，AI 也就看得见、查得动。把连接钉死在一个库上就没这回事了：
// 所有入口取的都是 src.Database，传别的库名一律拒绝。
//
// **诚实的边界**：这里做的是入口过滤——指定库时校验、SQL 里明写
// `别的库.表` 时拒绝。它挡不住动态 SQL、存储过程、或者把库名拼进字符串
// 再执行的写法。真正的边界始终是连接账号的授权范围：要让一条连接只碰得到
// 一个库，就给它配一个只授权那个库的 MySQL 账号。

// guardDatabase 校验调用方指定的库就是这条连接绑定的那个。
// 空表示「用这条连接的库」，一律放行。
func guardDatabase(src *model.DataSource, database string) error {
	database = strings.TrimSpace(database)
	if database == "" || strings.EqualFold(database, strings.TrimSpace(src.Database)) {
		return nil
	}
	return fmt.Errorf("%w: 连接 %s 只对应 %s 库，不能用它访问 %q",
		service.ErrForbidden, src.Ref, src.Database, database)
}

// guardStatements 拒绝明写了别的库的语句。
//
// 只认**表位置**（FROM/JOIN/INTO/UPDATE/TABLE 之后）的 `库名.` 限定名，
// 且在剥掉字符串与注释之后判断：`SELECT u.id FROM app.users u` 里的 `u.`
// 是表别名不是库引用，全局扫会把几乎每条 JOIN 都误判成跨库。
func guardStatements(src *model.DataSource, stmts []string) error {
	bound := strings.TrimSpace(src.Database)
	if bound == "" {
		return nil
	}
	for _, stmt := range stmts {
		for _, name := range qualifiedDatabases(stmt) {
			if !strings.EqualFold(name, bound) {
				return fmt.Errorf("%w: 语句引用了 %q 库，而连接 %s 只对应 %s 库",
					service.ErrForbidden, name, src.Ref, bound)
			}
		}
	}
	return nil
}

// dotSpacing 匹配点号周围的空白：`db . tbl` 与 `db.tbl` 在 MySQL 里等价。
var dotSpacing = regexp.MustCompile(`\s*\.\s*`)

// tableKeywords 是「下一个标识符出现在表位置」的关键字。
var tableKeywords = map[string]bool{
	"from": true, "join": true, "into": true, "update": true, "table": true,
}

// qualifiedDatabases 提取语句里表位置上的 `<库>.` 限定名。
func qualifiedDatabases(stmt string) []string {
	// 先把点号周围的空白压掉：MySQL 认 `` `db` . `tbl` ``，不压的话
	// 加个空格就绕过了整道闸门。
	tokens := identTokens(dotSpacing.ReplaceAllString(stripLiterals(stmt), "."))
	var out []string
	for i := 0; i+1 < len(tokens); i++ {
		if !tableKeywords[strings.ToLower(tokens[i])] {
			continue
		}
		// 表位置上的 `db.table`：点号前那段是库名。
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
// 库名检测必须在骨架上做：`WHERE note = 'other.users'` 里的是数据不是引用。
func stripLiterals(s string) string {
	var b strings.Builder
	for i, n := 0, len(s); i < n; {
		rest := s[i:]
		switch c := s[i]; {
		case c == '\'' || c == '"':
			b.WriteByte(' ')
			i = scanQuoted(s, i)
		case c == '`':
			// 反引号是标识符引用，内容要保留（`other-db`.users 是真跨库）。
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
