package system

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// claude 的套餐水位借 CLI 自己的 /usage 数据面：以 stream-json 模式拉起
// `claude -p`，往 stdin 发一条 get_usage 控制请求，读到 control_response
// 就收工。不直接打 claude.ai 的 usage 接口——那要从钥匙串取 OAuth 令牌，
// 而令牌几小时就过期（实测早上打开十有八九是过期的），自己刷新又会和
// CLI 抢同一个轮换的 refresh token，抢输的一方被登出。CLI 在这条路上
// 自己处理过期与刷新，实测过期令牌照样拿到数据、钥匙串顺手被续上。
//
// 代价是 get_usage 是 SDK 标为实验的控制请求（sdk.d.ts 里名字都带着
// DO_NOT_RELY_ON_THIS），形状变了这里跟着改；进程拉起约 2.5 秒，靠缓存兜。

// claudeUsageRequestID 是控制请求的 id，用来在输出流里认出属于我们的那条回应。
const claudeUsageRequestID = "acpp-quota"

var errClaudeMissing = errors.New("claude CLI 不在 PATH 里")

func (s *Service) fetchClaudeQuota(ctx context.Context) PlanQuota {
	run := s.quota.claudeRun
	if run == nil {
		run = runClaudeUsage
	}
	raw, err := run(ctx)
	if err != nil {
		return PlanQuota{Status: claudeErrorStatus(err), Error: err.Error()}
	}
	q, err := parseClaudeUsage(raw)
	if err != nil {
		return PlanQuota{Status: "error", Error: err.Error()}
	}
	return q
}

// claudeErrorStatus 把取数失败归类。CLI 没装与没登录都不是「坏了」，
// 界面要给的是引导而不是报错。
func claudeErrorStatus(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, errClaudeMissing):
		return "unavailable"
	case strings.Contains(msg, "not logged in"), strings.Contains(msg, "/login"),
		strings.Contains(msg, "authentication"), strings.Contains(msg, "invalid api key"):
		return "not_logged_in"
	}
	return "error"
}

// runClaudeUsage 拉一次 CLI 拿 /usage 的原始载荷。
//
// 启动参数都是为了「只做这一件事」：`--setting-sources ""` 不加载用户设置
// （否则 SessionStart 钩子会跑一遍），`--strict-mcp-config` + 空清单不起任何
// MCP 服务器，工作目录放临时目录免得在 ~/.claude/projects 里给本项目留
// 一条空会话。嵌套标记要摘掉——从 agent 终端里起的后端会把 CLAUDECODE
// 传下来，CLI 见了以为自己跑在另一个 agent 里，直接拒绝服务。
func runClaudeUsage(ctx context.Context) ([]byte, error) {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return nil, errClaudeMissing
	}
	cmd := exec.CommandContext(ctx, bin,
		"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--setting-sources", "", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`)
	cmd.Dir = os.TempDir()
	cmd.Env = withoutEnv(os.Environ(), "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SSE_PORT")
	cmd.Stdin = strings.NewReader(fmt.Sprintf(
		`{"type":"control_request","request_id":%q,"request":{"subtype":"get_usage"}}`+"\n",
		claudeUsageRequestID))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("claude stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start claude: %w", err)
	}

	payload, respErr := readClaudeUsageResponse(stdout)
	// 答案到手就不陪它收尾：stdin 早已读完，正常会自己退出；给一点宽限，
	// 超时再杀只是兜底（此时它顶多在写遥测）。
	go func() { _, _ = io.Copy(io.Discard, stdout) }()
	waitOrKill(cmd, 5*time.Second)

	if respErr != nil {
		return nil, respErr
	}
	if payload == nil {
		// 进程退了却没回应：多半是没登录或版本太老不认这条控制请求，
		// 把 stderr 的尾巴带出去让人看得出原因。
		return nil, fmt.Errorf("claude 没有返回用量数据：%s", tailString(strings.TrimSpace(stderr.String()), 300))
	}
	return payload, nil
}

// readClaudeUsageResponse 在 stream-json 输出里找我们那条控制请求的回应。
// 其余行（system 事件之类）一律跳过。
func readClaudeUsageResponse(r io.Reader) ([]byte, error) {
	scanner := bufio.NewScanner(r)
	// 载荷里的 behaviors 段能有几十 KB，默认 64K 的行缓冲不够用。
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		var line struct {
			Type     string `json:"type"`
			Response struct {
				Subtype   string          `json:"subtype"`
				RequestID string          `json:"request_id"`
				Error     string          `json:"error"`
				Response  json.RawMessage `json:"response"`
			} `json:"response"`
		}
		if json.Unmarshal(scanner.Bytes(), &line) != nil || line.Type != "control_response" ||
			line.Response.RequestID != claudeUsageRequestID {
			continue
		}
		if line.Response.Subtype != "success" {
			return nil, fmt.Errorf("claude: %s", line.Response.Error)
		}
		return append([]byte(nil), line.Response.Response...), nil
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读 claude 输出：%w", err)
	}
	return nil, nil
}

// waitOrKill 等进程自然退出，超过宽限就杀。
func waitOrKill(cmd *exec.Cmd, grace time.Duration) {
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(grace):
		_ = cmd.Process.Kill()
		<-done
	}
}

// withoutEnv 从环境里摘掉指定变量。
func withoutEnv(env []string, keys ...string) []string {
	drop := make(map[string]bool, len(keys))
	for _, k := range keys {
		drop[k] = true
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if !drop[key] {
			out = append(out, kv)
		}
	}
	return out
}

// claudeUsage 是 get_usage 回应里与限额有关的部分。字段名是 CLI 的。
type claudeUsage struct {
	SubscriptionType    *string `json:"subscription_type"`
	RateLimitsAvailable bool    `json:"rate_limits_available"`
	RateLimits          *struct {
		FiveHour       *claudeWindow `json:"five_hour"`
		SevenDay       *claudeWindow `json:"seven_day"`
		SevenDayOpus   *claudeWindow `json:"seven_day_opus"`
		SevenDaySonnet *claudeWindow `json:"seven_day_sonnet"`
		ModelScoped    []struct {
			DisplayName string `json:"display_name"`
			claudeWindow
		} `json:"model_scoped"`
		// Limits 是服务端给的窗口清单（桌面 app 的用量面板画的就是它）：
		// session / weekly_all / weekly_scoped（scope 里带型号名）。有它就
		// 以它为准，上面那些具名字段是没有 limits 的老版本兜底。
		Limits []struct {
			Kind     string   `json:"kind"`
			Percent  *float64 `json:"percent"`
			ResetsAt *string  `json:"resets_at"`
			IsActive bool     `json:"is_active"`
			Scope    *struct {
				Model *struct {
					DisplayName string `json:"display_name"`
				} `json:"model"`
			} `json:"scope"`
		} `json:"limits"`
		ExtraUsage *struct {
			IsEnabled    bool     `json:"is_enabled"`
			MonthlyLimit *float64 `json:"monthly_limit"`
			UsedCredits  *float64 `json:"used_credits"`
			Utilization  *float64 `json:"utilization"`
			Currency     *string  `json:"currency"`
		} `json:"extra_usage"`
	} `json:"rate_limits"`
}

type claudeWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

// parseClaudeUsage 把 get_usage 的载荷整理成 PlanQuota。
func parseClaudeUsage(raw []byte) (PlanQuota, error) {
	var u claudeUsage
	if err := json.Unmarshal(raw, &u); err != nil {
		return PlanQuota{}, fmt.Errorf("解析 claude 用量：%w", err)
	}
	q := PlanQuota{Status: "ok", Windows: []QuotaWindow{}}
	if u.SubscriptionType != nil {
		q.Plan = *u.SubscriptionType
	}
	if !u.RateLimitsAvailable || u.RateLimits == nil {
		// API key / Bedrock / Vertex 这类没有套餐限额，或者干脆没登录。
		q.Status = "unavailable"
		return q, nil
	}
	rl := u.RateLimits

	if len(rl.Limits) > 0 {
		for _, l := range rl.Limits {
			if l.Percent == nil {
				continue
			}
			w := QuotaWindow{Percent: *l.Percent, ResetsAt: parseClaudeTime(l.ResetsAt), Active: l.IsActive}
			switch l.Kind {
			case "session":
				w.Kind = "session"
			case "weekly_all":
				w.Kind = "weekly"
			case "weekly_scoped":
				w.Kind = "weekly_model"
				if l.Scope != nil && l.Scope.Model != nil {
					w.Model = l.Scope.Model.DisplayName
				}
			default:
				w.Kind = l.Kind
			}
			q.Windows = append(q.Windows, w)
		}
	} else {
		add := func(kind, model string, win *claudeWindow) {
			if win == nil || win.Utilization == nil {
				return
			}
			q.Windows = append(q.Windows, QuotaWindow{
				Kind: kind, Model: model, Percent: *win.Utilization, ResetsAt: parseClaudeTime(win.ResetsAt),
			})
		}
		add("session", "", rl.FiveHour)
		add("weekly", "", rl.SevenDay)
		add("weekly_model", "Opus", rl.SevenDayOpus)
		add("weekly_model", "Sonnet", rl.SevenDaySonnet)
		for i := range rl.ModelScoped {
			add("weekly_model", rl.ModelScoped[i].DisplayName, &rl.ModelScoped[i].claudeWindow)
		}
	}

	if ex := rl.ExtraUsage; ex != nil && ex.IsEnabled {
		extra := &QuotaExtra{}
		if ex.Utilization != nil {
			extra.Percent = *ex.Utilization
		}
		if ex.UsedCredits != nil {
			extra.Used = *ex.UsedCredits
		}
		if ex.MonthlyLimit != nil {
			extra.Limit = *ex.MonthlyLimit
		}
		if ex.Currency != nil {
			extra.Currency = *ex.Currency
		}
		q.Extra = extra
	}
	return q, nil
}

// parseClaudeTime 解析 CLI 给的 ISO 时间（带微秒与 +00:00 偏移）。
func parseClaudeTime(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, *s)
	if err != nil {
		return nil
	}
	return &t
}
