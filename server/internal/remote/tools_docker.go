package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"acpp/server/internal/mcp"
)

// Docker 面：容器在不在、日志说了什么。

const (
	logsDefaultTail = 200
	logsMaxTail     = 2000
	// logsSearchWindow 是带 grep 时往回翻的最大行数。再大就该去读挂载出来的
	// 文件日志了——docker 自己那份本来就有轮转上限，翻不了多远。
	logsSearchWindow = 20000
)

func (s *Service) dockerTools(pick picker) []mcp.Tool {
	return []mcp.Tool{{
		Name:        "docker_ps",
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		Description: "列出容器：名字、镜像、状态、已运行多久、**重启次数**、健康检查、端口。" +
			"「服务是不是挂了」从这里起步——重启次数在涨说明它起不来又被拉起，" +
			"那比日志里任何一行都先说明问题。镜像与启动时间还能用来确认某次发布到底上没上。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server": serverArg(),
				"all":    map[string]any{"type": "boolean", "description": "连已停止的一起列（默认只列在跑的）"},
				"filter": map[string]any{"type": "string", "description": "按名字子串过滤，一台机器上跑着多个项目时用它只看自己关心的那组"},
			},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args := parseArgs(raw)
			srv, err := pick(ctx, args.Server)
			if err != nil {
				return "", err
			}
			return s.text(ctx, srv, psCmd(args))
		},
	}, {
		Name:        "docker_logs",
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		Description: "看容器的 stdout 日志（docker 自己收的那份）。" +
			"注意这**只是近期**——docker 的日志有轮转上限，很多项目同时把完整日志写在挂载出来的目录里，" +
			"要查更早的就去读那些文件（用 server_ls 找、server_grep 搜）。" +
			"用 since 限时间窗（如 30m、2h）比拉大 tail 更省，也更容易看清一段时间内发生了什么。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server":     serverArg(),
				"container":  map[string]any{"type": "string", "description": "容器名，取自 docker_ps"},
				"tail":       map[string]any{"type": "integer", "description": "最后几行，默认 " + strconv.Itoa(logsDefaultTail) + "，最多 " + strconv.Itoa(logsMaxTail)},
				"since":      map[string]any{"type": "string", "description": "只看这段时间内的，如 30m / 2h / 2026-09-01T10:00:00"},
				"until":      map[string]any{"type": "string", "description": "只看这个时间点之前的，格式同 since"},
				"grep":       map[string]any{"type": "string", "description": "只留匹配这个正则的行——查特定错误时先过一遍，比整段拉回来省得多"},
				"timestamps": map[string]any{"type": "boolean", "description": "每行带上时间戳"},
			},
			"required": []string{"container"},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args := parseArgs(raw)
			srv, err := pick(ctx, args.Server)
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(args.Container) == "" {
				return "", fmt.Errorf("container 不能为空（先用 docker_ps 看有哪些）")
			}
			return s.text(ctx, srv, logsCmd(args))
		},
	}, {
		Name:        "docker_inspect",
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		Description: "看一个容器的详细配置：**挂载**、compose 标签（含远程项目根目录）、启动命令、" +
			"工作目录、重启策略、健康检查、退出码与 OOM 标记、网络模式。" +
			"这是**代码与服务器之间的桥**——compose 标签里的 working_dir 直接告诉你项目在这台机器的哪个目录，" +
			"挂载列表告诉你日志与配置被映射到了哪里，比翻目录靠谱得多。环境变量只列名字不列值。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server":    serverArg(),
				"container": map[string]any{"type": "string", "description": "容器名，取自 docker_ps"},
			},
			"required": []string{"container"},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args := parseArgs(raw)
			srv, err := pick(ctx, args.Server)
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(args.Container) == "" {
				return "", fmt.Errorf("container 不能为空")
			}
			return s.text(ctx, srv, inspectCmd(args))
		},
	}, {
		Name:        "docker_stats",
		Annotations: &mcp.Annotations{ReadOnlyHint: true},
		Description: "容器此刻的资源占用：CPU%、内存用量与占比、网络与块设备 IO、进程数。" +
			"确认「是哪个容器在吃资源」用它——server_info 只告诉你整机紧张，这里告诉你紧张在谁身上。" +
			"要采样一两秒，比别的工具慢。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server": serverArg(),
				"filter": map[string]any{"type": "string", "description": "按名字子串只看这一组容器"},
			},
		},
		Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args := parseArgs(raw)
			srv, err := pick(ctx, args.Server)
			if err != nil {
				return "", err
			}
			return s.text(ctx, srv, statsCmd(args))
		},
	}}
}

// inspectCmd 拼容器详情。
//
// **环境变量只列名字不列值**：容器的 env 里躺着数据库密码、API key、
// token——那些东西没有任何理由进入模型的上下文，而它们恰好又最容易
// 被顺手带出去（一次 inspect 就够）。想确认某个配置项存不存在，看名字
// 就够了；想知道值，那是人该去服务器上看的事。
func inspectCmd(a toolArgs) string {
	name := shellQuote(a.Container)
	return fmt.Sprintf(`
if ! command -v docker >/dev/null 2>&1; then echo '这台服务器上没有 docker'; exit 0; fi
c=%s
echo '## 基本'
docker inspect --format '名字: {{.Name}}
镜像: {{.Config.Image}}
状态: {{.State.Status}}  退出码: {{.State.ExitCode}}  OOM: {{.State.OOMKilled}}
重启: {{.RestartCount}} 次  策略: {{.HostConfig.RestartPolicy.Name}}
启动于: {{.State.StartedAt}}
命令: {{.Config.Cmd}}
工作目录: {{.Config.WorkingDir}}
网络模式: {{.HostConfig.NetworkMode}}' "$c" 2>&1
echo
echo '## compose 标签（项目根目录在这里）'
docker inspect --format '{{range $k, $v := .Config.Labels}}{{if or (eq $k "com.docker.compose.project") (eq $k "com.docker.compose.project.working_dir") (eq $k "com.docker.compose.service") (eq $k "com.docker.compose.project.config_files")}}{{$k}} = {{$v}}
{{end}}{{end}}' "$c" 2>/dev/null
echo '## 挂载（宿主 -> 容器）'
docker inspect --format '{{range .Mounts}}{{.Source}} -> {{.Destination}} ({{.Mode}})
{{end}}' "$c" 2>/dev/null | head -20
echo '## 健康检查'
docker inspect --format '{{if .State.Health}}状态: {{.State.Health.Status}}  连续失败: {{.State.Health.FailingStreak}}{{range $i, $l := .State.Health.Log}}
  最近: {{$l.ExitCode}} {{$l.Output}}{{end}}{{else}}（没有配健康检查）{{end}}' "$c" 2>/dev/null | head -8
echo
echo '## 环境变量（只列名字——值里可能有密码与 token）'
docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$c" 2>/dev/null | cut -d= -f1 | sort | head -40
`, name)
}

// statsCmd 拼资源占用。
//
// --no-stream 只采一次；不加的话它会一直刷。带 filter 时用管道喂 xargs
// 而不是把 id 塞进变量——理由同 psCmd：远端登录 shell 可能是 zsh，
// 未加引号的变量不会被分词。
func statsCmd(a toolArgs) string {
	format := "--format 'table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.NetIO}}\t{{.BlockIO}}\t{{.PIDs}}'"

	run := "docker stats --no-stream " + format + " 2>&1 | head -40"
	if f := strings.TrimSpace(a.Filter); f != "" {
		run = "docker ps --filter name=" + shellQuote(f) +
			" -q 2>/dev/null | xargs -r docker stats --no-stream " + format + " 2>&1 | head -40"
	}
	return fmt.Sprintf(`
if ! command -v docker >/dev/null 2>&1; then echo '这台服务器上没有 docker'; exit 0; fi
%s
`, run)
}

// psCmd 拼容器清单。
//
// 重启次数与健康状态要 inspect 才有，而那是每容器一次调用——所以用一条
// `docker inspect $(docker ps -q)` 批量取，再与 ps 的输出并排打印。
// 这两样正是排障第一眼要看的，值得多这一步。
func psCmd(a toolArgs) string {
	psArgs := "docker ps"
	if a.All {
		psArgs += " -a"
	}
	if f := strings.TrimSpace(a.Filter); f != "" {
		psArgs += " --filter name=" + shellQuote(f)
	}

	// tab 分隔，前端与模型都好读；Status 里本来就带 (healthy) 之类。
	format := `--format '{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'`
	ids := psArgs + " -q"

	return fmt.Sprintf(`
if ! command -v docker >/dev/null 2>&1; then echo '这台服务器上没有 docker'; exit 0; fi
echo '## 容器（名字 | 镜像 | 状态 | 端口）'
%s %s 2>&1 | head -n 60
echo
echo '## 重启次数（只列非 0 的——在涨说明它起不来又被反复拉起）'
ids=$(%s 2>/dev/null | head -n 60)
if [ -n "$ids" ]; then
  docker inspect --format '{{.Name}} {{.RestartCount}} {{if .State.Health}}{{.State.Health.Status}}{{end}}' $ids 2>/dev/null \
    | awk '$2 != 0 || $3 != "" {print}' | head -n 40
fi
`, psArgs, format, ids)
}

// logsCmd 拼容器日志命令。grep 在**服务端**过滤，不是拉回来再筛——
// 那正是输出预算要省的地方。
//
// 带 grep 时搜索窗口要比返回条数大得多：`--tail 3 | grep` 是在最后三行里
// 找，几乎必然一无所获（真机上就是这么发现的）。语义应当是「从最近这么一
// 大段里，把匹配的最后 N 条给我」，所以窗口按返回条数放大，再由末尾的
// tail 收到 N 条。
func logsCmd(a toolArgs) string {
	tail := clamp(a.Tail, logsDefaultTail, logsMaxTail)
	window := tail
	if strings.TrimSpace(a.Grep) != "" {
		window = clamp(tail*20, logsDefaultTail*20, logsSearchWindow)
	}
	cmd := fmt.Sprintf("docker logs --tail %d", window)
	if a.Timestamps {
		cmd += " -t"
	}
	if v := strings.TrimSpace(a.Since); v != "" {
		cmd += " --since " + shellQuote(v)
	}
	if v := strings.TrimSpace(a.Until); v != "" {
		cmd += " --until " + shellQuote(v)
	}
	// 2>&1：很多程序把日志写在 stderr，只取 stdout 会看到一片空白。
	cmd += " " + shellQuote(a.Container) + " 2>&1"

	if g := strings.TrimSpace(a.Grep); g != "" {
		cmd += " | grep -E -- " + shellQuote(g)
	}
	cmd += fmt.Sprintf(" | tail -n %d", tail)

	return fmt.Sprintf(`
if ! command -v docker >/dev/null 2>&1; then echo '这台服务器上没有 docker'; exit 0; fi
%s
`, nice(cmd))
}
