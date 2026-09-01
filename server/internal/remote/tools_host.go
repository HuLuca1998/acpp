package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"acpp/server/internal/mcp"
	"acpp/server/internal/model"
)

// 主机面：有哪些机器、这台机器现在什么状况。

func (s *Service) hostTools(sc Scope, pick picker) []mcp.Tool {
	return []mcp.Tool{{
		Name:        "server_hosts",
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		Description: "列出可以连的服务器：名字、地址与用途备注。" +
			"要看线上情况却不确定连哪台时先调它——备注里通常写着这台机器跑的是什么、项目在哪个目录。",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Call: func(ctx context.Context, _ json.RawMessage) (string, error) {
			list, err := s.visible(ctx, sc)
			if err != nil {
				return "", err
			}
			return renderHosts(list), nil
		},
	}, {
		Name:        "server_info",
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		Description: "一次拿全这台机器的现状：系统版本、开机时长、负载、CPU 核数、内存、各挂载点磁盘、当前时间。" +
			"判断「是不是资源不够了」从这里起步；" +
			"要跑重活（大范围 grep）之前也先看一眼负载——生产机上莽撞的全盘搜索会把正经服务挤出去。",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"server": serverArg()},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args := parseArgs(raw)
			srv, err := pick(ctx, args.Server)
			if err != nil {
				return "", err
			}
			return s.text(ctx, srv, infoCmd)
		},
	}, {
		Name:        "server_ps",
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		Description: "宿主上的进程，按 CPU 或内存排序。" +
			"**docker_stats 看不到的那些**——直接跑在宿主上的 nginx、mysql、各种 daemon 都在这里。" +
			"整机负载高但容器都不忙时，答案通常在这。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server": serverArg(),
				"sort":   map[string]any{"type": "string", "enum": []string{"cpu", "mem"}, "description": "排序依据，默认 cpu"},
				"filter": map[string]any{"type": "string", "description": "只看命令行里含这个字串的进程"},
				"limit":  map[string]any{"type": "integer", "description": "最多几条，默认 20"},
			},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args := parseArgs(raw)
			srv, err := pick(ctx, args.Server)
			if err != nil {
				return "", err
			}
			return s.text(ctx, srv, psHostCmd(args))
		},
	}, {
		Name:        "server_ports",
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		Description: "在听哪些端口、分别是谁在听。" +
			"确认「服务到底起没起」比看日志直接；也能发现只绑在回环上的内部端口（调试接口、指标端点之类）。" +
			"端口对不上代码里写的，多半是配置没生效或者跑的是旧版本。",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"server": serverArg()},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args := parseArgs(raw)
			srv, err := pick(ctx, args.Server)
			if err != nil {
				return "", err
			}
			return s.text(ctx, srv, portsCmd)
		},
	}, {
		Name:        "server_journal",
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		Description: "systemd 的服务日志（journalctl）。日志的第三种去处——" +
			"不是所有服务都跑在容器里，systemd 拉起的那些（定时任务、代理、守护进程）日志只在这。" +
			"不给 unit 就看全局最近的记录，适合查「机器上刚发生过什么」。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server":  serverArg(),
				"pattern": map[string]any{"type": "string", "description": "unit 名，如 nginx.service。留空看全局"},
				"since":   map[string]any{"type": "string", "description": "起点，如 '1 hour ago'、'today'、'2026-09-01 10:00'"},
				"grep":    map[string]any{"type": "string", "description": "只留匹配的行"},
				"limit":   map[string]any{"type": "integer", "description": "最多几行，默认 100"},
			},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args := parseArgs(raw)
			srv, err := pick(ctx, args.Server)
			if err != nil {
				return "", err
			}
			return s.text(ctx, srv, journalCmd(args))
		},
	}}
}

// psHostCmd 拼宿主进程清单。
//
// 用 `ps -eo` 显式指定列而不是 `ps aux`：后者的列顺序在不同实现里不一样，
// 而我们要按固定列排序。--sort 是 GNU procps 的，busybox 不认，退回 sort 命令。
func psHostCmd(a toolArgs) string {
	limit := clamp(a.Limit, 20, 100)
	key := "-%cpu"
	if a.Sort == "mem" {
		key = "-%mem"
	}
	filter := "cat"
	if f := strings.TrimSpace(a.Filter); f != "" {
		filter = "grep -F -- " + shellQuote(f)
	}
	// **不能写成 `nice -n 19 (…)`**：nice 后面跟子 shell 括号在 zsh 里是
	// 语法错误（真机上就是这么报的 parse error）。先探一次 --sort 能力，
	// 再让 nice 跟一条实实在在的命令。
	return fmt.Sprintf(`
if ps -eo pid --sort=-%%cpu >/dev/null 2>&1; then
  %s
else
  %s
fi`,
		nice(fmt.Sprintf("ps -eo pid,user,pcpu,pmem,etime,args --sort=%s 2>/dev/null | %s | head -n %d", key, filter, limit+1)),
		nice(fmt.Sprintf("ps aux 2>/dev/null | %s | head -n %d", filter, limit+1)))
}

// portsCmd 列监听端口。ss 是现在的标准工具，老系统上退回 netstat。
const portsCmd = `
(ss -tlnp 2>/dev/null || netstat -tlnp 2>/dev/null || echo '(ss 与 netstat 都不可用)') | head -50
`

// journalCmd 拼 systemd 日志命令。
func journalCmd(a toolArgs) string {
	limit := clamp(a.Limit, 100, 1000)
	cmd := fmt.Sprintf("journalctl --no-pager -n %d", limit)
	if u := strings.TrimSpace(a.Pattern); u != "" {
		cmd += " -u " + shellQuote(u)
	}
	if since := strings.TrimSpace(a.Since); since != "" {
		cmd += " --since " + shellQuote(since)
	}
	if g := strings.TrimSpace(a.Grep); g != "" {
		cmd += " | grep -E -- " + shellQuote(g)
	}
	return fmt.Sprintf(`
if ! command -v journalctl >/dev/null 2>&1; then echo '这台服务器没有 systemd/journalctl'; exit 0; fi
%s 2>&1 | tail -n %d
`, nice(cmd), limit)
}

// infoCmd 把七八条命令拼成一次往返。分开调的话，一次「看看机器怎么样」
// 就是七次 SSH 握手——而这些信息本来就该一起看。
//
// 每条都带 fallback：busybox 与 GNU 的参数不一样，`free`/`nproc` 在精简
// 系统里可能压根没有。取不到就说取不到，不要整条命令挂掉。
const infoCmd = `
echo '## 系统'
(grep -E '^(PRETTY_NAME|VERSION)=' /etc/os-release 2>/dev/null | head -2) || true
uname -sr 2>/dev/null || true
echo
echo '## 运行'
uptime 2>/dev/null || true
echo "时间: $(date '+%F %T %Z' 2>/dev/null)"
echo
echo '## CPU / 内存'
echo "CPU 核数: $(nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo '?')"
free -h 2>/dev/null || free 2>/dev/null || echo '(free 不可用)'
echo
echo '## 磁盘'
# 过滤 overlay/tmpfs：跑了几十个容器的机器上，docker 的 overlayfs 挂载能刷掉
# 十几行，而它们全指向同一块盘——真正要看的是宿主那几个挂载点。
(df -h -x tmpfs -x devtmpfs -x overlay -x squashfs 2>/dev/null \
  || df -h 2>/dev/null | grep -vE '^(overlay|shm|tmpfs|devtmpfs|udev)') | head -12
echo
echo '## 网络'
(cat /proc/net/dev 2>/dev/null | awk 'NR>2 && $2>0 {printf "%s 收 %.1fMB 发 %.1fMB\n", $1, $2/1048576, $10/1048576}' | head -5) || true
`

// renderHosts 把服务器清单渲染给模型看。
//
// 备注单独占一行而不是挤在后面：那是**唯一**告诉模型「这台机器是干嘛的」
// 的地方，而它往往是一整句话。
func renderHosts(list []model.Server) string {
	if len(list) == 0 {
		return "还没有配置任何服务器。去「服务器」页加一台，AI 才能看到线上情况。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "可用服务器 %d 台：\n", len(list))
	for i := range list {
		srv := &list[i]
		fmt.Fprintf(&b, "\n- %s  (%s@%s:%d)", srv.Name, srv.User, srv.Host, srv.Port)
		if note := strings.TrimSpace(srv.Note); note != "" {
			fmt.Fprintf(&b, "\n  %s", note)
		}
	}
	return b.String()
}
