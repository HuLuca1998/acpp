package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"acpp/server/internal/mcp"
)

// 文件面：列目录、读文件、搜内容。
//
// 形状刻意对标模型已有的本地工具（Read 的 offset/limit、Grep 的
// pattern/context）——同一套心智的远程版，模型不用学新东西就会用。

const (
	lsDefaultLimit  = 200
	lsMaxLimit      = 1000
	lsMaxDepth      = 3
	readDefaultLine = 200
	readMaxLine     = 2000
	grepDefaultMax  = 50
	grepMaxMax      = 200
)

func (s *Service) fileTools(pick picker) []mcp.Tool {
	return []mcp.Tool{{
		Name:        "server_ls",
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		Description: "列目录内容：名称、大小、修改时间、权限，软链接标出指向。" +
			"读一个日志目录之前先用它——**文件多大、最后写于什么时候**决定了接下来该 tail 还是该 grep，" +
			"直接去读一个几百 MB 的文件是最常见的浪费。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server":  serverArg(),
				"path":    map[string]any{"type": "string", "description": "目录的绝对路径"},
				"depth":   map[string]any{"type": "integer", "description": "递归层数，默认 1（只看当前层），最多 " + strconv.Itoa(lsMaxDepth)},
				"pattern": map[string]any{"type": "string", "description": "文件名通配，如 *.log"},
				"sort":    map[string]any{"type": "string", "enum": []string{"name", "size", "mtime"}, "description": "排序方式，默认按名字；找「最近在写的日志」用 mtime，找「谁占了磁盘」用 size"},
				"limit":   map[string]any{"type": "integer", "description": "最多返回几条，默认 " + strconv.Itoa(lsDefaultLimit)},
			},
			"required": []string{"path"},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args := parseArgs(raw)
			srv, err := pick(ctx, args.Server)
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(args.Path) == "" {
				return "", fmt.Errorf("path 不能为空")
			}
			return s.text(ctx, srv, lsCmd(args))
		},
	}, {
		Name:        "server_read",
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		Description: "读文件内容，带行号。**看日志优先用 tail**（要最近发生的事），" +
			"要定位具体某段才用 offset+limit。" +
			"返回带文件总行数与大小，好判断自己看到的是全部还是一角；" +
			"二进制文件会被拒绝。单次最多带回 32KB，不够就收窄范围再读一次，不要反复整读。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server": serverArg(),
				"path":   map[string]any{"type": "string", "description": "文件的绝对路径"},
				"tail":   map[string]any{"type": "integer", "description": "只要最后 N 行——看日志几乎总该用它"},
				"offset": map[string]any{"type": "integer", "description": "从第几行开始（1 起），与 limit 配合读中间某段"},
				"limit":  map[string]any{"type": "integer", "description": "读多少行，默认 " + strconv.Itoa(readDefaultLine) + "，最多 " + strconv.Itoa(readMaxLine)},
			},
			"required": []string{"path"},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args := parseArgs(raw)
			srv, err := pick(ctx, args.Server)
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(args.Path) == "" {
				return "", fmt.Errorf("path 不能为空")
			}
			return s.text(ctx, srv, readCmd(args))
		},
	}, {
		Name:        "server_grep",
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		Description: "在文件或目录里搜内容，返回 文件:行号:内容。定位报错、找某个 id 的踪迹用它。" +
			"**代价意识**：线上日志目录可能有几个 GB，搜之前先想清楚搜哪个文件、" +
			"用 include 限定文件名、给 maxMatches 设一个够用的小数——命中够数就停，不会把整个目录读完。" +
			"搜到之后想看上下文，用 context 参数或改用 server_read 读那一段。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server":     serverArg(),
				"path":       map[string]any{"type": "string", "description": "要搜的文件或目录（绝对路径）"},
				"pattern":    map[string]any{"type": "string", "description": "正则（扩展正则语法）"},
				"ignoreCase": map[string]any{"type": "boolean", "description": "忽略大小写"},
				"context":    map[string]any{"type": "integer", "description": "同时给出命中行前后各几行"},
				"include":    map[string]any{"type": "string", "description": "只搜匹配这个通配的文件，如 *.log。搜目录时**务必给**，否则会连二进制一起翻"},
				"maxMatches": map[string]any{"type": "integer", "description": "最多命中几条就停，默认 " + strconv.Itoa(grepDefaultMax) + "，最多 " + strconv.Itoa(grepMaxMax)},
			},
			"required": []string{"path", "pattern"},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args := parseArgs(raw)
			srv, err := pick(ctx, args.Server)
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(args.Path) == "" || strings.TrimSpace(args.Pattern) == "" {
				return "", fmt.Errorf("path 与 pattern 都不能为空")
			}
			return s.text(ctx, srv, grepCmd(args))
		},
	}}
}

// lsCmd 拼列目录的命令。
//
// depth=1 用 ls，更深用 find——find 能递归但输出格式不如 ls 好读，
// 所以只在真的要递归时才用它。
func lsCmd(a toolArgs) string {
	path := shellQuote(a.Path)
	limit := clamp(a.Limit, lsDefaultLimit, lsMaxLimit)
	depth := clamp(a.Depth, 1, lsMaxDepth)

	sortFlag := ""
	switch a.Sort {
	case "size":
		sortFlag = " -S"
	case "mtime":
		sortFlag = " -t"
	}

	if depth <= 1 {
		name := ""
		if p := strings.TrimSpace(a.Pattern); p != "" {
			// 通配交给远端 shell 展开，所以模式本身不能被引号裹死；
			// 用 find 在单层里做匹配，避免把模式当字面文件名。
			name = fmt.Sprintf(" -maxdepth 1 -name %s", shellQuote(p))
			return nice(fmt.Sprintf(
				"find %s%s -mindepth 1 2>&1 | head -n %d | xargs -r ls -ldh --time-style=long-iso 2>/dev/null || find %s%s -mindepth 1 2>&1 | head -n %d",
				path, name, limit, path, name, limit))
		}
		// --time-style 是 GNU 的；不认就退回默认格式（busybox）。
		return nice(fmt.Sprintf(
			"ls -lAh%s --time-style=long-iso %s 2>&1 | head -n %d || ls -lAh%s %s 2>&1 | head -n %d",
			sortFlag, path, limit+1, sortFlag, path, limit+1))
	}

	name := ""
	if p := strings.TrimSpace(a.Pattern); p != "" {
		name = fmt.Sprintf(" -name %s", shellQuote(p))
	}
	return nice(fmt.Sprintf(
		"find %s -maxdepth %d%s -mindepth 1 2>&1 | head -n %d",
		path, depth, name, limit))
}

// readCmd 拼读文件的命令。
//
// 先报文件本身的信息（大小、总行数），再给内容——模型据此知道自己看到的
// 是全部还是一角。二进制在服务端就挡掉：把一段 ELF 灌进上下文毫无意义。
func readCmd(a toolArgs) string {
	path := shellQuote(a.Path)
	limit := clamp(a.Limit, readDefaultLine, readMaxLine)

	var body string
	switch {
	case a.Tail > 0:
		n := clamp(a.Tail, readDefaultLine, readMaxLine)
		// 带上真实行号：tail 自己不给，用总行数减回去。
		body = fmt.Sprintf(
			"tail -n %d %s | nl -ba -v $(( total > %d ? total - %d + 1 : 1 ))",
			n, path, n, n)
	case a.Offset > 0:
		end := a.Offset + limit - 1
		body = fmt.Sprintf("sed -n '%d,%dp' %s | nl -ba -v %d", a.Offset, end, path, a.Offset)
	default:
		body = fmt.Sprintf("head -n %d %s | nl -ba", limit, path)
	}

	return nice(fmt.Sprintf(`
if [ ! -e %s ]; then echo "没有这个文件：%s"; exit 0; fi
if [ -d %s ]; then echo "这是个目录，用 server_ls 看它"; exit 0; fi
if LC_ALL=C grep -qI . %s 2>/dev/null; then :; else echo "二进制文件，不适合读取内容"; exit 0; fi
total=$(wc -l < %s 2>/dev/null || echo 0)
size=$(ls -lh %s 2>/dev/null | awk '{print $5}')
echo "# %s（共 ${total} 行，${size}）"
echo
%s
`, path, escapeEcho(a.Path), path, path, path, path, escapeEcho(a.Path), body))
}

// grepCmd 拼搜索命令。
//
// 三道限流缺一不可：`-m` 让每个文件命中够数就停、`head` 兜住总行数、
// nice 让它不跟业务抢 CPU。少任何一道，一次「查查最近有什么错误」都可能
// 在几 GB 的日志目录上跑成一次事故。
func grepCmd(a toolArgs) string {
	maxMatches := clamp(a.MaxMatches, grepDefaultMax, grepMaxMax)
	flags := []string{"-n", "-E", "-I"}
	if a.IgnoreCase {
		flags = append(flags, "-i")
	}
	if a.Context > 0 {
		flags = append(flags, "-C", strconv.Itoa(clamp(a.Context, 2, 10)))
	}
	flags = append(flags, "-m", strconv.Itoa(maxMatches))

	target := shellQuote(a.Path)
	rec := ""
	if inc := strings.TrimSpace(a.Include); inc != "" {
		rec = " -r --include=" + shellQuote(inc)
	} else {
		// 没给 include 时仍允许搜目录（-r），但 -I 会跳过二进制，
		// 且总行数被 head 兜住。描述里已经劝了要给 include。
		rec = " -r"
	}

	return nice(fmt.Sprintf(
		"grep %s%s -e %s %s 2>&1 | head -n %d",
		strings.Join(flags, " "), rec, shellQuote(a.Pattern), target, maxMatches*3))
}

// escapeEcho 让路径能安全地出现在双引号 echo 里（只用于回显，不参与执行）。
func escapeEcho(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`)
	return r.Replace(s)
}
