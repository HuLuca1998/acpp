package db

import "strings"

// LikePattern 把用户输入变成 `LIKE ? ESCAPE '\\'` 的子串模式：先把 `\`、`%`、`_`
// 逃逸掉再两头加 `%`。不逃逸的话输入一个 `%` 就匹配全部，`_` 会当成单字符
// 通配——搜「a_b」得到「acb」是那种很难被发现的错。
func LikePattern(keyword string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(strings.TrimSpace(keyword)) + "%"
}
