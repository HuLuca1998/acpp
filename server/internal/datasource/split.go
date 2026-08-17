package datasource

import "strings"

// Split 把一段脚本按分隔符切成多条语句。
//
// 为什么不直接把整段丢给驱动的 multiStatements：那样只能拿到一个合并
// 结果，哪条失败、哪条改了多少行全看不见。自己切分之后每条语句都有独立
// 的耗时、影响行数与错误位置，AI 和人拿到的都是可定位的反馈。
//
// 处理的语法：单引号 / 双引号 / 反引号字符串（反斜杠转义与叠写转义都认）、
// `-- ` 与 `#` 行注释、`/* */` 块注释（`/*! */` 版本化注释保留执行），
// 以及 `DELIMITER` 指令——存储过程与触发器的函数体里全是分号，没有它
// 就只能一条条手工执行。
func Split(script string) []string {
	var (
		stmts []string
		buf   strings.Builder
		delim = ";"
	)

	flush := func() {
		if s := strings.TrimSpace(buf.String()); s != "" {
			stmts = append(stmts, s)
		}
		buf.Reset()
	}

	for i, n := 0, len(script); i < n; {
		// DELIMITER 只在语句边界上认——语句中间出现的同名标识符不该被当指令。
		if strings.TrimSpace(buf.String()) == "" {
			if d, skip, ok := parseDelimiter(script[i:]); ok {
				delim = d
				buf.Reset()
				i += skip
				continue
			}
		}

		rest := script[i:]
		switch c := script[i]; {
		case c == '\'' || c == '"' || c == '`':
			j := scanQuoted(script, i)
			buf.WriteString(script[i:j])
			i = j

		// `--` 只有后面跟空白（或行尾）才是注释，`a--b` 里是两个减号。
		case c == '-' && strings.HasPrefix(rest, "--") &&
			(len(rest) == 2 || rest[2] == ' ' || rest[2] == '\t' || rest[2] == '\n' || rest[2] == '\r'):
			i = skipLine(script, i)

		case c == '#':
			i = skipLine(script, i)

		case c == '/' && strings.HasPrefix(rest, "/*"):
			j := skipBlockComment(script, i)
			// /*!40101 ... */ 是 MySQL 的版本化注释，内容是要执行的语句，
			// 当成普通注释丢掉会让 mysqldump 导出的脚本行为变样。
			if strings.HasPrefix(rest, "/*!") {
				buf.WriteString(script[i:j])
			} else {
				buf.WriteByte(' ')
			}
			i = j

		case strings.HasPrefix(rest, delim):
			flush()
			i += len(delim)

		default:
			buf.WriteByte(c)
			i++
		}
	}
	flush()
	return stmts
}

// parseDelimiter 识别行首的 `DELIMITER x` 指令，返回新分隔符与消费的字节数。
func parseDelimiter(s string) (string, int, bool) {
	const kw = "delimiter"
	if len(s) <= len(kw) || !strings.EqualFold(s[:len(kw)], kw) {
		return "", 0, false
	}
	i := len(kw)
	// 关键字后必须跟空格或 tab，否则是 `delimiters` 之类的标识符。
	if s[i] != ' ' && s[i] != '\t' {
		return "", 0, false
	}
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	start := i
	for i < len(s) && s[i] != '\n' && s[i] != '\r' {
		i++
	}
	d := strings.TrimSpace(s[start:i])
	if d == "" {
		return "", 0, false
	}
	return d, i, true
}

// scanQuoted 从引号起始位置扫到闭合引号之后，返回结束下标。
// 未闭合时返回字符串末尾——把残缺片段原样交给数据库报错，
// 比在这里猜用户想写什么更诚实。
func scanQuoted(s string, start int) int {
	q := s[start]
	for i := start + 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			// 反引号里没有反斜杠转义（MySQL 标识符引用规则）。
			if q != '`' {
				i++
			}
		case q:
			// 叠写即转义：'' 、"" 、`` 都表示一个字面引号。
			if i+1 < len(s) && s[i+1] == q {
				i++
				continue
			}
			return i + 1
		}
	}
	return len(s)
}

// skipLine 跳到行尾（不含换行符本身——换行留在流里，保证前后 token 不粘连）。
func skipLine(s string, i int) int {
	for i < len(s) && s[i] != '\n' {
		i++
	}
	return i
}

func skipBlockComment(s string, i int) int {
	if end := strings.Index(s[i+2:], "*/"); end >= 0 {
		return i + 2 + end + 2
	}
	return len(s)
}
