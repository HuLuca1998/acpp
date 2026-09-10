// Package ghcli 是本机 gh CLI 的薄封装：定位可执行文件、跑一条命令、把
// 「没装」「没登录」翻译成可以直接给用户看的错误。
//
// 项目里 project（仓库清单）、github（issue 列表）都要跑 gh，各写一份
// 找路径与译错误的逻辑只会长歪——尤其打包成 .app 之后进程继承的是 launchd
// 的精简 PATH，homebrew 的 bin 不在里面，漏掉这一条的那份就会「本机明明装
// 了却说没装」。不 import 本项目其他包。
package ghcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ErrNotInstalled 表示本机找不到 gh。
var ErrNotInstalled = errors.New("gh CLI not installed")

// ErrNotLoggedIn 表示 gh 装了但没有登录态。
var ErrNotLoggedIn = errors.New("gh CLI not logged in (run `gh auth login`)")

// Timeout 是单次 gh 调用的上限：GitHub API 偶发挂起时不该把调用方拖死。
const Timeout = 30 * time.Second

// Run 跑一条 gh 命令，返回 stdout。失败时 stderr 的最后几行进错误文本——
// 「exit status 1」对着界面没有任何意义。
func Run(ctx context.Context, args ...string) ([]byte, error) {
	bin := Find()
	if bin == "" {
		return nil, ErrNotInstalled
	}
	cctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	msg := tailLines(stderr.String(), 3)
	switch {
	case cctx.Err() != nil && ctx.Err() == nil:
		return nil, fmt.Errorf("gh timed out after %s", Timeout)
	case strings.Contains(msg, "auth login") || strings.Contains(msg, "authentication"):
		return nil, ErrNotLoggedIn
	case msg != "":
		return nil, fmt.Errorf("gh: %s", msg)
	}
	return nil, fmt.Errorf("run gh: %w", err)
}

// Find 定位 gh 可执行文件；PATH 之外还翻 homebrew 与系统的常见落点。
func Find() string {
	if p, err := exec.LookPath("gh"); err == nil {
		return p
	}
	for _, p := range []string{"/opt/homebrew/bin/gh", "/usr/local/bin/gh", "/usr/bin/gh"} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

func tailLines(output string, n int) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
