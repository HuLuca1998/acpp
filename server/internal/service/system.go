package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/gorm"

	"acpp/server/internal/config"
)

// SystemService 提供系统级配置：数据目录的查看与迁移。
type SystemService struct {
	db  *gorm.DB
	cfg config.Config
}

func NewSystemService(gdb *gorm.DB, cfg config.Config) *SystemService {
	return &SystemService{db: gdb, cfg: cfg}
}

// SystemInfo 是设置面板展示的数据目录状态。
type SystemInfo struct {
	// DataDir 是当前进程实际使用的数据目录。
	DataDir string `json:"dataDir"`
	// DefaultDir 是默认数据目录（~/.acpp）。
	DefaultDir string `json:"defaultDir"`
	// PendingDir 非空表示已迁移到新目录、等待重启生效。
	PendingDir string `json:"pendingDir,omitempty"`
}

func (s *SystemService) Info() SystemInfo {
	info := SystemInfo{
		DataDir:    s.cfg.DataDir,
		DefaultDir: config.ConfigHome(),
	}
	if saved := config.SavedDataDir(); saved != "" && !samePath(saved, s.cfg.DataDir) {
		info.PendingDir = saved
	}
	return info
}

// MigrateDataDir 把数据迁到新目录：sqlite 用 VACUUM INTO 做在线一致
// 快照，转录逐文件拷贝（append-only，拷贝安全），最后写固定配置文件。
// 旧数据留在原地不动；新目录在下次启动时生效。
func (s *SystemService) MigrateDataDir(ctx context.Context, target string) (SystemInfo, error) {
	target = filepath.Clean(target)
	if !filepath.IsAbs(target) {
		return SystemInfo{}, fmt.Errorf("%w: data dir must be an absolute path", ErrInvalid)
	}
	if samePath(target, s.cfg.DataDir) {
		return SystemInfo{}, fmt.Errorf("%w: already using this directory", ErrInvalid)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return SystemInfo{}, fmt.Errorf("create target dir: %w", err)
	}
	targetDB := filepath.Join(target, "acp.db")
	if _, err := os.Stat(targetDB); err == nil {
		// 覆盖别人的数据库比失败糟糕得多，让用户换个目录或清空它。
		return SystemInfo{}, fmt.Errorf("%w: target already contains acp.db", ErrInvalid)
	}

	if err := s.db.WithContext(ctx).Exec("VACUUM INTO ?", targetDB).Error; err != nil {
		return SystemInfo{}, fmt.Errorf("snapshot database: %w", err)
	}
	if err := config.CopyDirFiles(s.cfg.TranscriptDir, filepath.Join(target, "transcripts")); err != nil {
		return SystemInfo{}, fmt.Errorf("copy transcripts: %w", err)
	}
	if err := config.SaveDataDir(target); err != nil {
		return SystemInfo{}, err
	}
	info := s.Info()
	info.PendingDir = target
	return info, nil
}

func samePath(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && aa == bb
}
