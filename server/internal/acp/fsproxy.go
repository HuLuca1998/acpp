package acp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadTextFile 代理 agent 的读文件请求，路径限制在会话 cwd 内。
func (h *sessionHandler) ReadTextFile(_ context.Context, p ReadTextFileParams) (ReadTextFileResult, error) {
	path, err := h.session.guardPath(p.Path)
	if err != nil {
		return ReadTextFileResult{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ReadTextFileResult{}, fmt.Errorf("read %s: %w", p.Path, err)
	}

	content := string(data)
	if p.Line != nil || p.Limit != nil {
		content = sliceLines(content, p.Line, p.Limit)
	}
	return ReadTextFileResult{Content: content}, nil
}

// WriteTextFile 代理 agent 的写文件请求，路径限制在会话 cwd 内。
func (h *sessionHandler) WriteTextFile(_ context.Context, p WriteTextFileParams) error {
	path, err := h.session.guardPath(p.Path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent of %s: %w", p.Path, err)
	}
	if err := os.WriteFile(path, []byte(p.Content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", p.Path, err)
	}
	return nil
}

// guardPath 把路径解析到 canonical 形式并确认它在 cwd 之内。
//
// 这条防线只覆盖走 fs 代理的操作：claude 走，codex 用自带 shell 完全不走。
// 所以它是纵深防御的一层，不是全部。
func (s *Session) guardPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.cwd, path)
	}
	clean := filepath.Clean(path)

	root, err := filepath.EvalSymlinks(s.cwd)
	if err != nil {
		root = filepath.Clean(s.cwd)
	}
	// 目标可能还不存在（写新文件），所以对父目录求 canonical 路径。
	resolvedDir, err := filepath.EvalSymlinks(filepath.Dir(clean))
	if err != nil {
		resolvedDir = filepath.Dir(clean)
	}
	resolved := filepath.Join(resolvedDir, filepath.Base(clean))

	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside the session working directory", path)
	}
	return resolved, nil
}

func sliceLines(content string, line, limit *int) string {
	lines := strings.Split(content, "\n")
	start := 0
	if line != nil && *line > 0 {
		start = *line - 1
	}
	if start >= len(lines) {
		return ""
	}
	end := len(lines)
	if limit != nil && *limit > 0 && start+*limit < end {
		end = start + *limit
	}
	return strings.Join(lines[start:end], "\n")
}
