package service

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// 技能附属文件（references/ scripts/ assets/ 等）的管理。SKILL.md 走结构化
// 编辑（skill.go），其余文件按普通文本文件读写；二进制只列出不可编辑——
// 页面没有上传二进制的场景，需要时用户直接放进源目录即可被识别。

// SkillFile 是附属文件列表项，Path 是技能目录内的相对路径。
type SkillFile struct {
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	Binary    bool      `json:"binary"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// SkillFileContent 追加文本内容；二进制文件不可获取。
type SkillFileContent struct {
	SkillFile
	Content string `json:"content"`
}

// maxSkillFileSize 是可编辑文件的上限；技能文件是给模型读的文本，
// 超过说明放错了东西。
const maxSkillFileSize = 1 << 20

func (s *SkillService) ListFiles(name string) ([]SkillFile, error) {
	if err := s.check(name); err != nil {
		return nil, err
	}
	root := filepath.Join(s.srcDir, name)
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("skill %s: %w", name, ErrNotFound)
	}

	files := []SkillFile{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "SKILL.md" || strings.HasPrefix(filepath.Base(rel), ".") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files = append(files, SkillFile{
			Path:      rel,
			Size:      info.Size(),
			Binary:    isBinaryFile(p),
			UpdatedAt: info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list skill files %s: %w", name, err)
	}
	return files, nil
}

func (s *SkillService) GetFile(name, rel string) (*SkillFileContent, error) {
	full, err := s.filePath(name, rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(full)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("skill file %s/%s: %w", name, rel, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("stat skill file %s/%s: %w", name, rel, err)
	}
	if info.Size() > maxSkillFileSize {
		return nil, fmt.Errorf("%w: file too large to edit (%d bytes)", ErrInvalid, info.Size())
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return nil, fmt.Errorf("read skill file %s/%s: %w", name, rel, err)
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return nil, fmt.Errorf("%w: binary file is not editable", ErrInvalid)
	}
	return &SkillFileContent{
		SkillFile: SkillFile{Path: path.Clean(rel), Size: info.Size(), UpdatedAt: info.ModTime()},
		Content:   string(raw),
	}, nil
}

// PutFile 写入（不存在则创建，父目录自动补齐）。
func (s *SkillService) PutFile(name, rel, content string) (*SkillFile, error) {
	full, err := s.filePath(name, rel)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Lstat(filepath.Join(s.srcDir, name)); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("skill %s: %w", name, ErrNotFound)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return nil, fmt.Errorf("create skill file dir: %w", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("write skill file %s/%s: %w", name, rel, err)
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, fmt.Errorf("stat skill file %s/%s: %w", name, rel, err)
	}
	return &SkillFile{Path: path.Clean(rel), Size: info.Size(), UpdatedAt: info.ModTime()}, nil
}

func (s *SkillService) DeleteFile(name, rel string) error {
	full, err := s.filePath(name, rel)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Lstat(full); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("skill file %s/%s: %w", name, rel, ErrNotFound)
	}
	if err := os.Remove(full); err != nil {
		return fmt.Errorf("delete skill file %s/%s: %w", name, rel, err)
	}
	// 顺手清掉变空的中间目录，最多清到技能根为止；失败无所谓。
	for dir := filepath.Dir(full); dir != filepath.Join(s.srcDir, name); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			break
		}
	}
	return nil
}

// filePath 把相对路径钉死在技能目录内：拒绝绝对路径、.. 穿越、隐藏段与
// SKILL.md（它走结构化编辑，不给旁路绕过 frontmatter 校验）。
func (s *SkillService) filePath(name, rel string) (string, error) {
	if err := s.check(name); err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	cleaned := path.Clean(rel)
	if cleaned == "" || cleaned == "." || path.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("skill file %s/%s: %w", name, rel, ErrNotFound)
	}
	for seg := range strings.SplitSeq(cleaned, "/") {
		if strings.HasPrefix(seg, ".") {
			return "", fmt.Errorf("skill file %s/%s: %w", name, rel, ErrNotFound)
		}
	}
	if cleaned == "SKILL.md" {
		return "", fmt.Errorf("%w: SKILL.md is managed via the skill editor", ErrInvalid)
	}
	return filepath.Join(s.srcDir, name, filepath.FromSlash(cleaned)), nil
}

func isBinaryFile(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	return bytes.IndexByte(buf[:n], 0) >= 0
}
