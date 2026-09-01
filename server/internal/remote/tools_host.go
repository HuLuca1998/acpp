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
	}}
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
