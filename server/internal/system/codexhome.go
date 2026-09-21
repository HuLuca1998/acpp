package system

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"acpp/server/internal/service"
)

// codex 的隔离 home：acpp 把 CODEX_HOME 重定向到 <dataDir>/codex-home，
// 机器级 ~/.codex 完全不在会话视野里（见 README 的技能库一节）。代价是那
// 两个真正要改的文件也跟着藏起来了：
//
//   - config.toml 是系统配置的**一次性副本**，此后系统那份的改动不再同步
//     ——要给 acpp 的 codex 换模型或 provider，改的就是这一份；
//   - auth.json 软链系统的那份（跟随登录态，不复制密钥）。
//
// 所以这里开一个窄口子：只读写这两个文件，外加在访达里打开那个目录。

// codexHomeFiles 是允许读写的文件白名单。不接受任意路径——这个面存在的
// 理由只是「那两个文件不好找」，不是给一个文件管理器。
var codexHomeFiles = []string{"config.toml", "auth.json"}

// CodexFile 是 codex home 里的一个文件。
type CodexFile struct {
	Name string `json:"name"`
	// Exists 为假时 Size / UpdatedAt 无意义：config.toml 只在第一次起
	// codex 会话时才被复制出来，auth.json 也要登录过才有。
	Exists    bool      `json:"exists"`
	Size      int64     `json:"size"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Symlink 为真表示它是个软链（auth.json 正是），Target 是指向哪儿。
	// 界面要据此提醒：改它等于改系统那一份。
	Symlink bool   `json:"symlink"`
	Target  string `json:"target,omitempty"`
}

// CodexHomeInfo 是这个面的概览。
type CodexHomeInfo struct {
	Dir   string      `json:"dir"`
	Files []CodexFile `json:"files"`
}

// CodexHome 返回 codex home 的位置与那两个文件的状态。
func (s *Service) CodexHome() CodexHomeInfo {
	dir := s.codexHomeDir()
	info := CodexHomeInfo{Dir: dir, Files: make([]CodexFile, 0, len(codexHomeFiles))}
	for _, name := range codexHomeFiles {
		file := CodexFile{Name: name}
		full := filepath.Join(dir, name)
		if st, err := os.Lstat(full); err == nil {
			file.Exists = true
			file.Symlink = st.Mode()&os.ModeSymlink != 0
			if file.Symlink {
				if target, err := os.Readlink(full); err == nil {
					file.Target = target
				}
				// 软链的大小是链接本身，要的是目标文件的。
				if st, err := os.Stat(full); err == nil {
					file.Size, file.UpdatedAt = st.Size(), st.ModTime()
				}
			} else {
				file.Size, file.UpdatedAt = st.Size(), st.ModTime()
			}
		}
		info.Files = append(info.Files, file)
	}
	return info
}

// ReadCodexFile 读一个文件的内容。
func (s *Service) ReadCodexFile(name string) (string, error) {
	full, err := s.codexFilePath(name)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(full)
	if os.IsNotExist(err) {
		// 还没生成不是错误：config.toml 要等第一次起 codex 会话才被复制
		// 出来。回空内容，界面照样能编辑并保存出一份。
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", name, err)
	}
	return string(raw), nil
}

// WriteCodexFile 覆盖写一个文件。
//
// **软链是顺着写的**（auth.json 就是软链到系统 ~/.codex/auth.json）：那正是
// 用户在这个面上想做的事——改系统登录态，而不是把软链换成普通文件。界面
// 会先说清楚这一点。
func (s *Service) WriteCodexFile(name, content string) error {
	full, err := s.codexFilePath(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("create codex home: %w", err)
	}
	// 0o600：这两个文件里是凭证与 provider 配置，跟着 codex 自己的口径走。
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

// RevealCodexHome 在访达里打开这个目录。只有 macOS 有意义——桌面版本来
// 就是 macOS 应用；别的平台明确报错，而不是悄悄什么都不做。
func (s *Service) RevealCodexHome() error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("%w: 只有 macOS 能在访达里打开目录", service.ErrInvalid)
	}
	dir := s.codexHomeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create codex home: %w", err)
	}
	if err := exec.Command("open", dir).Run(); err != nil {
		return fmt.Errorf("open %s: %w", dir, err)
	}
	return nil
}

func (s *Service) codexHomeDir() string {
	return filepath.Join(s.cfg.DataDir, "codex-home")
}

// codexFilePath 把名字钉死在白名单上：这个面不接受任意路径。
func (s *Service) codexFilePath(name string) (string, error) {
	name = strings.TrimSpace(name)
	for _, allowed := range codexHomeFiles {
		if name == allowed {
			return filepath.Join(s.codexHomeDir(), name), nil
		}
	}
	return "", fmt.Errorf("%w: 只能读写 %s", service.ErrInvalid, strings.Join(codexHomeFiles, " / "))
}
