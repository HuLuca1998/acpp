package system

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// codex 的套餐水位打 ChatGPT 后台的 usage 接口——codex CLI 的 /status 显示
// 的「5h limit / weekly limit」就是从这里来的。凭证用 codex 自己的 auth.json
//（acpp 的 codex-home 里那份软链着系统登录态），只读不写：令牌过期不自己
// 刷新，理由与 claude 一样（抢刷会把用户登出）；codex 的 access token 以
// 周计，跑一次 codex 就续上，界面提示即可。
//
// 注意这份额度是**账号**的：acpp 里的 codex 若配了第三方 provider，消耗的
// 不是它——Provider 字段就是为了把这句话带到界面上。

const codexUsageAPI = "https://chatgpt.com/backend-api/wham/usage"

func (s *Service) fetchCodexQuota(ctx context.Context) PlanQuota {
	raw, err := os.ReadFile(s.codexAuthPath())
	if os.IsNotExist(err) {
		return PlanQuota{Status: "not_logged_in"}
	}
	if err != nil {
		return PlanQuota{Status: "error", Error: fmt.Sprintf("读 auth.json：%v", err)}
	}
	var auth struct {
		AuthMode string `json:"auth_mode"`
		Tokens   *struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(raw, &auth); err != nil {
		return PlanQuota{Status: "error", Error: fmt.Sprintf("解析 auth.json：%v", err)}
	}
	if auth.Tokens == nil || auth.Tokens.AccessToken == "" {
		if auth.AuthMode == "apikey" {
			// API key 走的是按量计费，没有套餐窗口可看。
			return PlanQuota{Status: "unavailable"}
		}
		return PlanQuota{Status: "not_logged_in"}
	}
	if exp, ok := jwtExpiry(auth.Tokens.AccessToken); ok && time.Now().After(exp) {
		return PlanQuota{Status: "expired"}
	}

	api := s.quota.codexAPI
	if api == "" {
		api = codexUsageAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return PlanQuota{Status: "error", Error: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+auth.Tokens.AccessToken)
	if auth.Tokens.AccountID != "" {
		req.Header.Set("ChatGPT-Account-Id", auth.Tokens.AccountID)
	}
	req.Header.Set("User-Agent", "acpp")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return PlanQuota{Status: "error", Error: fmt.Sprintf("请求 codex 用量：%v", err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return PlanQuota{Status: "error", Error: fmt.Sprintf("读 codex 用量：%v", err)}
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		// 令牌还没到 exp 却被拒了：多半是被撤销或换过设备，与过期同一种处理。
		return PlanQuota{Status: "expired"}
	case resp.StatusCode != http.StatusOK:
		return PlanQuota{Status: "error", Error: fmt.Sprintf("codex 用量接口返回 %d：%s", resp.StatusCode, tailString(strings.TrimSpace(string(body)), 200))}
	}
	q, err := parseCodexUsage(body)
	if err != nil {
		return PlanQuota{Status: "error", Error: err.Error()}
	}
	q.Provider = codexProvider(filepath.Join(s.codexHomeDir(), "config.toml"))
	return q
}

// codexAuthPath 定位 auth.json：优先 acpp 的 codex-home（软链系统那份），
// 还没起过 codex 会话时 home 尚未搭起来，退到系统的 ~/.codex。
func (s *Service) codexAuthPath() string {
	local := filepath.Join(s.codexHomeDir(), "auth.json")
	if _, err := os.Stat(local); err == nil {
		return local
	}
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		if dir, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(dir, ".codex")
		}
	}
	return filepath.Join(home, "auth.json")
}

// codexUsage 是 usage 接口回应里用得上的部分。字段名是服务端的。
type codexUsage struct {
	PlanType  string `json:"plan_type"`
	RateLimit *struct {
		Primary   *codexWindow `json:"primary_window"`
		Secondary *codexWindow `json:"secondary_window"`
	} `json:"rate_limit"`
	Credits *struct {
		HasCredits bool   `json:"has_credits"`
		Unlimited  bool   `json:"unlimited"`
		Balance    string `json:"balance"`
	} `json:"credits"`
}

type codexWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int     `json:"limit_window_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

// parseCodexUsage 把 usage 接口的回应整理成 PlanQuota。
func parseCodexUsage(raw []byte) (PlanQuota, error) {
	var u codexUsage
	if err := json.Unmarshal(raw, &u); err != nil {
		return PlanQuota{}, fmt.Errorf("解析 codex 用量：%w", err)
	}
	q := PlanQuota{Status: "ok", Plan: u.PlanType, Windows: []QuotaWindow{}}
	if u.RateLimit != nil {
		for _, win := range []*codexWindow{u.RateLimit.Primary, u.RateLimit.Secondary} {
			if win == nil {
				continue
			}
			w := QuotaWindow{
				Kind:          windowKind(win.LimitWindowSeconds),
				WindowSeconds: win.LimitWindowSeconds,
				Percent:       win.UsedPercent,
			}
			if win.ResetAt > 0 {
				t := time.Unix(win.ResetAt, 0)
				w.ResetsAt = &t
			}
			q.Windows = append(q.Windows, w)
		}
	}
	if c := u.Credits; c != nil && (c.HasCredits || c.Unlimited) {
		q.Credits = &QuotaCredits{Balance: c.Balance, Unlimited: c.Unlimited}
	}
	return q, nil
}

// jwtExpiry 读 JWT 的 exp。只是为了在本地先判过期、省一次注定 401 的请求，
// 不校验签名——校验是服务端的事。解不出来就当不知道，照常去请求。
func jwtExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp == 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Exp, 0), true
}

// providerLine 匹配 config.toml 顶层的 model_provider。表内不会有同名键，
// 顶层这一行就是全部；正经解析 toml 要多拉一个依赖，为一个键不值。
var providerLine = regexp.MustCompile(`(?m)^\s*model_provider\s*=\s*"([^"]*)"`)

// codexProvider 返回 acpp 的 codex 配的第三方 provider 名；没配或就是
// openai 官方时返回空——那时消耗的正是这份额度，不必多话。
func codexProvider(configPath string) string {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	m := providerLine.FindSubmatch(raw)
	if m == nil {
		return ""
	}
	name := strings.TrimSpace(string(m[1]))
	if name == "openai" {
		return ""
	}
	return name
}
