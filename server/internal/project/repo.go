package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"acpp/server/internal/service"
)

// ghTimeout 是单次 gh 调用的上限：清单是个交互式操作，等超过这个时间
// 用户早就手输 URL 了。
const ghTimeout = 15 * time.Second

// repoCacheTTL 是仓库清单的缓存时长。清单变化很慢，而每次打开克隆对话框
// 都去打一次 GitHub API 既慢又白费配额。
const repoCacheTTL = time.Minute

// Repo 是一个可克隆的远端仓库。
type Repo struct {
	// Name 是 `<组织>/<仓库>`，同时也是克隆到工作区后的项目名。
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Private     bool      `json:"private"`
	CloneURL    string    `json:"cloneUrl"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

var repoCache struct {
	mu      sync.Mutex
	repos   []Repo
	fetched time.Time
}

// Repos 列出 gh 登录账号可访问的仓库，**排除个人账号名下的**（adr-007）。
//
// 过滤靠 GitHub 的 affiliation 参数完成：只要 organization_member 与
// collaborator 两种关系，owner 关系压根不请求——比拉全量再筛更可靠，
// 也不会因为分页把个人仓库漏筛进来。
func Repos(ctx context.Context) ([]Repo, error) {
	repoCache.mu.Lock()
	defer repoCache.mu.Unlock()

	if time.Since(repoCache.fetched) < repoCacheTTL && repoCache.repos != nil {
		return repoCache.repos, nil
	}

	raw, err := gh(ctx, "api", "-H", "Accept: application/vnd.github+json",
		"/user/repos?affiliation=organization_member,collaborator&sort=updated&per_page=100")
	if err != nil {
		return nil, err
	}

	var payload []struct {
		FullName    string    `json:"full_name"`
		Description string    `json:"description"`
		Private     bool      `json:"private"`
		CloneURL    string    `json:"clone_url"`
		UpdatedAt   time.Time `json:"updated_at"`
		Owner       struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse gh repo list: %w", err)
	}

	// 再挡一道：登录账号自己名下的仓库永远不进清单，哪怕 API 口径变了。
	self := currentLogin(ctx)

	repos := make([]Repo, 0, len(payload))
	for _, item := range payload {
		if self != "" && strings.EqualFold(item.Owner.Login, self) {
			continue
		}
		repos = append(repos, Repo{
			Name:        item.FullName,
			Description: item.Description,
			Private:     item.Private,
			CloneURL:    item.CloneURL,
			UpdatedAt:   item.UpdatedAt,
		})
	}

	repoCache.repos = repos
	repoCache.fetched = time.Now()
	return repos, nil
}

// currentLogin 取当前 gh 登录名；拿不到就返回空串（少一道兜底过滤，
// 但 affiliation 本身已经保证不含个人仓库）。
func currentLogin(ctx context.Context) string {
	raw, err := gh(ctx, "api", "user")
	if err != nil {
		return ""
	}
	var user struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(raw, &user); err != nil {
		return ""
	}
	return user.Login
}

// gh 跑一条 gh 命令。没装或没登录都翻译成可直接显示给用户的话——
// 「exit status 1」对着界面没有任何意义。
func gh(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, ghTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}

	if errors.Is(err, exec.ErrNotFound) {
		return nil, fmt.Errorf("%w: gh CLI not installed", service.ErrInvalid)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		stderr := tailLines(string(exitErr.Stderr), 3)
		if strings.Contains(stderr, "auth login") || strings.Contains(stderr, "authentication") {
			return nil, fmt.Errorf("%w: gh CLI not logged in (run `gh auth login`)", service.ErrInvalid)
		}
		return nil, fmt.Errorf("%w: gh: %s", service.ErrInvalid, stderr)
	}
	return nil, fmt.Errorf("run gh: %w", err)
}
