package discord

import (
	"regexp"
	"strings"
)

// agent 的回复是标准 markdown，而 Discord 只认一个小方言：表格完全不渲染
//（原样竖线文本）、#### 及更深的标题不认、`- [ ]` 任务列表不认、---
// 分隔线不认。发出去之前先把这些翻译成 Discord 能看的形状——转译只动
// 围栏代码块**之外**的行，代码原文一个字都不能改。

var (
	deepHeading = regexp.MustCompile(`^#{4,6}\s+(.*)$`)
	taskOpen    = regexp.MustCompile(`^(\s*)[-*] \[ \] `)
	taskDone    = regexp.MustCompile(`^(\s*)[-*] \[[xX]\] `)
	horizRule   = regexp.MustCompile(`^\s*(-{3,}|\*{3,}|_{3,})\s*$`)
	tableRow    = regexp.MustCompile(`^\s*\|.*\|\s*$`)
	tableSep    = regexp.MustCompile(`^\s*\|?[\s:|-]+\|?\s*$`)
)

// mdToDiscord 把一段 markdown 翻译成 Discord 方言。
func mdToDiscord(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	inCode := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCode = !inCode
			out = append(out, line)
			continue
		}
		if inCode {
			out = append(out, line)
			continue
		}

		// 表格：连续的 |…| 行（第二行是 |---| 分隔）→ 等宽代码块。
		if tableRow.MatchString(line) && i+1 < len(lines) &&
			tableRow.MatchString(lines[i+1]) && tableSep.MatchString(lines[i+1]) {
			j := i
			var rows [][]string
			for j < len(lines) && tableRow.MatchString(lines[j]) {
				if !tableSep.MatchString(lines[j]) {
					rows = append(rows, splitTableRow(lines[j]))
				}
				j++
			}
			out = append(out, renderTableBlock(rows)...)
			i = j - 1
			continue
		}

		if m := deepHeading.FindStringSubmatch(line); m != nil {
			out = append(out, "**"+strings.TrimSpace(m[1])+"**")
			continue
		}
		if horizRule.MatchString(line) && !strings.HasPrefix(strings.TrimSpace(line), "***") {
			// ***粗斜体*** 开头的正常行不在此列；纯分隔线直接去掉。
			continue
		}
		line = taskDone.ReplaceAllString(line, "${1}✅ ")
		line = taskOpen.ReplaceAllString(line, "${1}☐ ")
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// splitTableRow 把 `| a | b |` 拆成单元格。
func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	cells := strings.Split(line, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

// renderTableBlock 把表格渲染成代码块里的等宽对齐文本。中日韩全角字符
// 按两倍宽度对齐，否则列会歪。
func renderTableBlock(rows [][]string) []string {
	if len(rows) == 0 {
		return nil
	}
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	widths := make([]int, cols)
	for _, r := range rows {
		for c, cell := range r {
			if w := displayWidth(cell); w > widths[c] {
				widths[c] = w
			}
		}
	}
	var b []string
	b = append(b, "```")
	for ri, r := range rows {
		var sb strings.Builder
		for c := 0; c < cols; c++ {
			cell := ""
			if c < len(r) {
				cell = r[c]
			}
			sb.WriteString(cell)
			if c < cols-1 {
				sb.WriteString(strings.Repeat(" ", widths[c]-displayWidth(cell)+2))
			}
		}
		b = append(b, strings.TrimRight(sb.String(), " "))
		if ri == 0 && len(rows) > 1 {
			total := 0
			for c, w := range widths {
				total += w
				if c < cols-1 {
					total += 2
				}
			}
			b = append(b, strings.Repeat("─", total))
		}
	}
	b = append(b, "```")
	return b
}

// displayWidth 估算终端等宽显示宽度：CJK 统一表意/全角/假名算 2。
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		switch {
		case r >= 0x1100 && (r <= 0x115F || // Hangul Jamo
			(r >= 0x2E80 && r <= 0xA4CF) || // CJK 部首到 Yi
			(r >= 0xAC00 && r <= 0xD7A3) || // Hangul 音节
			(r >= 0xF900 && r <= 0xFAFF) || // CJK 兼容表意
			(r >= 0xFE30 && r <= 0xFE4F) || // CJK 兼容形式
			(r >= 0xFF00 && r <= 0xFF60) || // 全角
			(r >= 0xFFE0 && r <= 0xFFE6) ||
			(r >= 0x20000 && r <= 0x3FFFD)):
			w += 2
		default:
			w++
		}
	}
	return w
}
