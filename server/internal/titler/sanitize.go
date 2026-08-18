package titler

import (
	"regexp"
	"strings"
)

// 小模型会犯的错这里全都当它一定会犯：带思考块、加引号书名号、写
// 「标题：」前缀、markdown 加粗、列表编号、末尾拖句号、多输出一段解释。
// 语法级约束靠不住（实测 ollama 的 format 参数对 mlx 后端不生效），
// 所以净化是唯一可靠的一道，宁可判废也不放脏标题进库。

var (
	// 未闭合的 </think> 也要认：think:false 失效时模型可能只吐半截。
	thinkRe  = regexp.MustCompile(`(?s)<think>.*?(</think>|$)`)
	prefixRe = regexp.MustCompile(`^\s*(?i:标题|title|会话标题|主题)\s*[:：]\s*`)
	// 列表编号：模型偶尔把标题当成清单第一项吐出来。符号项要求后面跟空白，
	// 否则 `**加粗**` 的第一个星号会被当成列表符吃掉（RE2 无负向断言，
	// 只能靠这个空白要求把两者分开）。
	bulletRe = regexp.MustCompile(`^\s*(?:[-*+•]\s+|\d+[.、)]\s*)`)
	spaceRe  = regexp.MustCompile(`\s+`)
)

// 成对包裹符：只在首尾配对出现时才剥，单边出现的是标题内容的一部分。
var wrapPairs = [][2]string{
	{`"`, `"`}, {`'`, `'`}, {"`", "`"},
	{`“`, `”`}, {`‘`, `’`}, {`《`, `》`}, {`「`, `」`}, {`『`, `』`},
	{`**`, `**`}, {`【`, `】`}, {`(`, `)`}, {`（`, `）`},
}

// 尾部标点：中文标题不带标点是硬要求，句号问号一律削掉。
const trailPunct = "。．，、！？；：.,!?;:~…"

// Sanitize 把模型返回的原始文本收拾成可用的会话标题，超长按字符截断。
// 返回空串表示这次生成作废——调用方应保留原有标题，不要写空进库。
func Sanitize(raw string, maxRunes int) string {
	s := thinkRe.ReplaceAllString(raw, "")

	// 只认第一段有内容的行：模型爱在标题后面再补一句解释。
	s = firstNonEmptyLine(s)

	// 前缀与包裹要反复剥——「**标题：xxx**」这种是套了两层的。
	for range 4 {
		before := s
		s = strings.TrimSpace(s)
		s = bulletRe.ReplaceAllString(s, "")
		s = prefixRe.ReplaceAllString(s, "")
		s = stripWrap(s)
		s = strings.Trim(s, trailPunct)
		if s == before {
			break
		}
	}

	// 内部换行已在取首行时排除，剩下的连续空白压成单个空格。
	s = strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))

	if maxRunes > 0 {
		if r := []rune(s); len(r) > maxRunes {
			// 截断后可能新露出一个尾部标点，再削一次。
			s = strings.TrimRight(strings.TrimSpace(string(r[:maxRunes])), trailPunct)
		}
	}
	return strings.TrimSpace(s)
}

func firstNonEmptyLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// stripWrap 剥掉一层首尾配对的包裹符。只剥配对的：单边引号（如
// 「他说"跑不动"」被截断后剩的半个）不该被当成包裹。
func stripWrap(s string) string {
	for _, p := range wrapPairs {
		open, close := p[0], p[1]
		if len(s) > len(open)+len(close) &&
			strings.HasPrefix(s, open) && strings.HasSuffix(s, close) {
			return strings.TrimSpace(s[len(open) : len(s)-len(close)])
		}
	}
	return s
}
