package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// 技能脚本（scripts/ 目录）的元信息解析与试运行。脚本头部用注释键值声明
// 参数，页面据此渲染控件；这里不发明新格式，只认连续注释行里的固定前缀：
//
//	#!/usr/bin/env python3
//	# desc: 校验 SKILL.md frontmatter 是否符合规范
//	# usage: validate.py <skill-dir> [--strict]
//	# arg: skill-dir 技能目录路径
//	# opt: strict 严格模式（勾选后以 --strict 传入）
//	# env: ACPP_DEBUG 打开调试输出
//
// # 与 // 两种注释都认；头部扫描到第一个非注释、非空行为止。

// SkillScript 是一个脚本的元信息，Args/Opts/Envs 驱动页面控件。
type SkillScript struct {
	Path        string           `json:"path"`
	Description string           `json:"description"`
	Usage       string           `json:"usage"`
	Args        []SkillScriptVar `json:"args"`
	Opts        []SkillScriptVar `json:"opts"`
	Envs        []SkillScriptVar `json:"envs"`
	// Runnable 为 false 表示扩展名不在解释器映射里，页面只展示不给运行。
	Runnable bool `json:"runnable"`
}

type SkillScriptVar struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// SkillScriptRunInput 是试运行入参：Args 按声明顺序的取值（空串跳过）、
// Opts 是勾选的开关名、Env 是环境变量键值。
type SkillScriptRunInput struct {
	Path string            `json:"path"`
	Args []string          `json:"args"`
	Opts []string          `json:"opts"`
	Env  map[string]string `json:"env"`
}

// SkillScriptRunResult 是试运行结果；输出超限会被截断并标记。
type SkillScriptRunResult struct {
	ExitCode   int    `json:"exitCode"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"durationMs"`
	TimedOut   bool   `json:"timedOut"`
	Truncated  bool   `json:"truncated"`
}

// 解释器按扩展名映射；不认识的扩展名标记为不可运行而不是猜 shebang——
// 猜错的失败模式（当 shell 跑 python)比明确拒绝糟糕得多。
var scriptInterpreters = map[string][]string{
	".py":  {"python3"},
	".sh":  {"bash"},
	".mjs": {"node"},
	".js":  {"node"},
}

const (
	scriptRunTimeout  = 60 * time.Second
	scriptOutputLimit = 256 << 10
)

// ListScripts 返回 scripts/ 下全部脚本的元信息，没有 scripts/ 目录时为空。
func (s *SkillService) ListScripts(name string) ([]SkillScript, error) {
	files, err := s.ListFiles(name)
	if err != nil {
		return nil, err
	}
	scripts := []SkillScript{}
	for _, f := range files {
		if !strings.HasPrefix(f.Path, "scripts/") || f.Binary {
			continue
		}
		full := filepath.Join(s.srcDir, name, filepath.FromSlash(f.Path))
		meta := parseScriptHeader(full)
		meta.Path = f.Path
		_, meta.Runnable = scriptInterpreters[strings.ToLower(filepath.Ext(f.Path))]
		scripts = append(scripts, meta)
	}
	return scripts, nil
}

// RunScript 以技能目录为 cwd 试运行脚本。这是本地开发工具，脚本本来就归
// 用户所有；风险控制在超时与输出上限，不做沙箱。
func (s *SkillService) RunScript(ctx context.Context, name string, in SkillScriptRunInput) (*SkillScriptRunResult, error) {
	if !strings.HasPrefix(in.Path, "scripts/") {
		return nil, fmt.Errorf("%w: only files under scripts/ are runnable", ErrInvalid)
	}
	full, err := s.filePath(name, in.Path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(full); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("skill script %s/%s: %w", name, in.Path, ErrNotFound)
	}
	interp, ok := scriptInterpreters[strings.ToLower(filepath.Ext(in.Path))]
	if !ok {
		return nil, fmt.Errorf("%w: no interpreter for %s", ErrInvalid, filepath.Ext(in.Path))
	}

	argv := append(append([]string{}, interp...), full)
	for _, a := range in.Args {
		if a != "" {
			argv = append(argv, a)
		}
	}
	for _, o := range in.Opts {
		o = strings.TrimSpace(strings.TrimPrefix(o, "--"))
		if o != "" {
			argv = append(argv, "--"+o)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, scriptRunTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = filepath.Join(s.srcDir, name)
	cmd.Env = os.Environ()
	for k, v := range in.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	result := &SkillScriptRunResult{
		Stdout:     truncateOutput(&stdout),
		Stderr:     truncateOutput(&stderr),
		DurationMs: time.Since(start).Milliseconds(),
		TimedOut:   errors.Is(ctx.Err(), context.DeadlineExceeded),
		Truncated:  stdout.Len() > scriptOutputLimit || stderr.Len() > scriptOutputLimit,
	}
	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
		// exitCode 0
	case errors.As(runErr, &exitErr):
		result.ExitCode = exitErr.ExitCode()
	default:
		// 解释器都没起来（未安装等），按输入问题报给用户。
		return nil, fmt.Errorf("%w: %v", ErrInvalid, runErr)
	}
	return result, nil
}

func truncateOutput(b *bytes.Buffer) string {
	if b.Len() <= scriptOutputLimit {
		return b.String()
	}
	return b.String()[:scriptOutputLimit]
}

// parseScriptHeader 解析脚本头部的注释元信息；读不出就返回零值，脚本
// 照样列出（页面提示补头部）。
func parseScriptHeader(path string) SkillScript {
	raw, err := os.ReadFile(path)
	if err != nil {
		return SkillScript{}
	}

	meta := SkillScript{}
	for i, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if i == 0 && strings.HasPrefix(trimmed, "#!") {
			continue
		}
		var comment string
		switch {
		case strings.HasPrefix(trimmed, "#"):
			comment = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		case strings.HasPrefix(trimmed, "//"):
			comment = strings.TrimSpace(strings.TrimLeft(trimmed, "/"))
		case trimmed == "":
			continue
		default:
			return meta // 头部结束
		}
		if after, ok := strings.CutPrefix(comment, "desc:"); ok {
			meta.Description = strings.TrimSpace(after)
		} else if after, ok := strings.CutPrefix(comment, "usage:"); ok {
			meta.Usage = strings.TrimSpace(after)
		} else if after, ok := strings.CutPrefix(comment, "arg:"); ok {
			meta.Args = append(meta.Args, splitVar(after))
		} else if after, ok := strings.CutPrefix(comment, "opt:"); ok {
			v := splitVar(after)
			v.Name = strings.TrimPrefix(v.Name, "--")
			meta.Opts = append(meta.Opts, v)
		} else if after, ok := strings.CutPrefix(comment, "env:"); ok {
			meta.Envs = append(meta.Envs, splitVar(after))
		}
	}
	return meta
}

// splitVar 拆 "name 描述文字"；没有描述时 Label 空着由前端兜底。
func splitVar(s string) SkillScriptVar {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		return SkillScriptVar{}
	}
	return SkillScriptVar{Name: fields[0], Label: strings.Join(fields[1:], " ")}
}
