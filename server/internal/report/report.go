// Package report 是「把报告摊开给人看」的能力面：agent 写完一份单文件
// HTML 报告后调一个工具，报告就在用户的工作区里打开。
//
// 这个包**不存任何东西**。报告是磁盘上的文件（agent 用它自己的写文件
// 工具产出，留在会话工作目录里，用户能自己打开、能提交、能发给别人），
// 「哪些报告属于这条会话」的事实源是转录里的 tool_call——刷新页面、换
// 台设备、局域网访客打开同一条会话，看到的都是同一组报告。所以这里既
// 没有库表也没有内存态，只做两件事：把工具挂给 agent，以及在调用时把
// 路径校验干净。
//
// 为什么工具只收路径、不收 HTML 全文：一份像样的报告轻松上万 token，
// 当参数传等于在 agent 上下文里存两份（它写的时候一份、工具调用回显一
// 份），改一个数字还要重发全文。让 agent 用自己的 Write/Edit 写文件，
// 这里只认路径，它就能用 Edit 增量改，改完再调一次同一路径即更新。
package report

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"acpp/server/internal/model"
)

// Sessions 是本包对会话仓储的最小依赖：由 MCP token 反查会话，以及为
// 会话准备回连用的 token。用接口而不是具体类型，理由与 datasource 一致
// ——业务包之间不互相 import。
type Sessions interface {
	SessionByMCPToken(ctx context.Context, token string) (uint, string, error)
	EnsureMCPToken(ctx context.Context, sessionID uint) (string, error)
}

// Calls 是调用观测的最小依赖。记录是尽力而为的旁路，所以没有返回值：
// 观测失败不该让 AI 的工具调用跟着失败。为 nil 时不记。
type Calls interface {
	Record(ctx context.Context, rec model.MCPCall)
}

// Notifier 是「报告已经打开了」这件事的广播口。
//
// 为什么要后端主动推，而不是让前端从 tool_call 事件里认：ACP 的
// rawInput 是**流式累积**的（实测：先到半个参数对象，补完之后末帧又变回
// null），前端要从这种流里稳定捞出 path 很脆，而两条 runtime 的分片行为
// 还不一定一样。后端在这里是确定地知道 sessionID 与路径的，推一条事件
// 比让前端猜可靠得多。
//
// 可为 nil（不广播，工具照常返回成功——报告文件已经写好了，界面没弹出来
// 也不该让这次调用变成失败）。
type Notifier interface {
	ReportOpened(sessionID uint, path, title string)
}

// Service 是报告能力的业务面。
type Service struct {
	sessions Sessions
	calls    Calls
	notifier Notifier
	// mcpBase 是 agent 回连的 MCP 端点前缀
	// （http://127.0.0.1:<port>/api/mcp/report/）。
	mcpBase string
}

func NewService(sessions Sessions, addr string) *Service {
	return &Service{sessions: sessions, mcpBase: mcpBaseURL(addr)}
}

// WithNotifier 挂上广播口。装配期调用一次，之后只读。
func (s *Service) WithNotifier(n Notifier) *Service {
	s.notifier = n
	return s
}

// WithCalls 挂上调用观测。分开一个 setter 而不是塞进 NewService：
// 记录是可选旁路，缺了报告功能照常跑。
func (s *Service) WithCalls(calls Calls) *Service {
	s.calls = calls
	return s
}

// mcpBaseURL 从监听地址推导 MCP 前缀：agent 子进程与我们同机，
// 监听 0.0.0.0 时也走回环回连。
func mcpBaseURL(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		port = "48080"
	}
	return "http://127.0.0.1:" + port + "/api/mcp/report/"
}

// resolveInCwd 把工具传来的路径解析成一个**确认落在会话工作目录内**的
// 绝对路径。
//
// 这不是防君子的检查，是这个工具唯一的护栏。acpp 有局域网分享与租户
// （adr-007），没有它，租户会话里一句 report_open("~/.ssh/id_rsa") 就能
// 把 owner 的私钥渲染到自己屏幕上。思路与 datasource 用 scope 把 AI 锁
// 在本项目库里一致：不是靠工具描述里的君子协定，是它**根本读不到**。
//
// 软链接必须先解析再比对：工作目录里放一条指向 /etc 的软链是完全合法
// 的操作，只比字符串前缀会被它绕过去。
func resolveInCwd(cwd, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path 不能为空")
	}
	if strings.TrimSpace(cwd) == "" {
		return "", fmt.Errorf("这条会话没有工作目录，无法定位报告")
	}

	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, abs)
	}
	abs = filepath.Clean(abs)

	// 真实路径：软链解析失败（比如文件不存在）时保留原值，让后面的
	// Stat 去报「文件不存在」这个更有用的错，而不是含糊的解析失败。
	realPath := abs
	if p, err := filepath.EvalSymlinks(abs); err == nil {
		realPath = p
	}
	realCwd := filepath.Clean(cwd)
	if p, err := filepath.EvalSymlinks(realCwd); err == nil {
		realCwd = p
	}

	if realPath != realCwd && !strings.HasPrefix(realPath, realCwd+string(os.PathSeparator)) {
		return "", fmt.Errorf("只能打开会话工作目录内的文件；%s 在目录之外", path)
	}

	info, err := os.Stat(realPath)
	if err != nil {
		return "", fmt.Errorf("打不开 %s：%w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s 是目录，不是报告文件", path)
	}
	if ext := strings.ToLower(filepath.Ext(realPath)); ext != ".html" && ext != ".htm" {
		return "", fmt.Errorf("只能打开 .html 报告，%s 不是", path)
	}
	return realPath, nil
}

// relTo 把绝对路径折回相对工作目录的形式，前端就是按这个形态定位文件的。
func relTo(cwd, abs string) string {
	realCwd := filepath.Clean(cwd)
	if p, err := filepath.EvalSymlinks(realCwd); err == nil {
		realCwd = p
	}
	if rel, err := filepath.Rel(realCwd, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return abs
}
