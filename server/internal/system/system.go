package system

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/gorm"

	"acpp/server/internal/config"

	"acpp/server/internal/service"
)

// Service 提供系统级配置：数据目录的查看与迁移。
type Service struct {
	db  *gorm.DB
	cfg config.Config
}

func NewService(gdb *gorm.DB, cfg config.Config) *Service {
	return &Service{db: gdb, cfg: cfg}
}

// SystemInfo 是设置面板展示的数据目录状态。
type SystemInfo struct {
	// DataDir 是当前进程实际使用的数据目录。
	DataDir string `json:"dataDir"`
	// DefaultDir 是默认数据目录（~/.acpp）。
	DefaultDir string `json:"defaultDir"`
	// PendingDir 非空表示已迁移到新目录、等待重启生效。
	PendingDir string `json:"pendingDir,omitempty"`
	// WorkspaceDir 是 agent 干活的地方：新会话工作目录的默认落点，也是
	// 局域网访客各自 root 的父目录（adr-007）。与数据目录刻意分开——
	// 数据目录装 db、转录与技能包，不该被 agent 当工作区写。
	WorkspaceDir string `json:"workspaceDir"`
	// DefaultWorkspaceDir 是工作区根的默认值（~/acpp）。
	DefaultWorkspaceDir string `json:"defaultWorkspaceDir"`
}

func (s *Service) Info() SystemInfo {
	info := SystemInfo{
		DataDir:             s.cfg.DataDir,
		DefaultDir:          config.ConfigHome(),
		WorkspaceDir:        service.DefaultCwd(),
		DefaultWorkspaceDir: config.DefaultWorkspaceDir(),
	}
	if saved := config.SavedDataDir(); saved != "" && !config.SamePath(saved, s.cfg.DataDir) {
		info.PendingDir = saved
	}
	return info
}

// SetWorkspaceDir 改工作区根。与数据目录不同，它**立刻生效**：不涉及
// 已打开的数据库，只影响之后新建会话的默认落点与新建租户的 root。
// 已有会话与已有租户的目录不动——它们的路径已经写进记录了。
func (s *Service) SetWorkspaceDir(dir string) (SystemInfo, error) {
	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) {
		return SystemInfo{}, fmt.Errorf("%w: workspace dir must be an absolute path", service.ErrInvalid)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return SystemInfo{}, fmt.Errorf("create workspace dir: %w", err)
	}
	if err := config.SaveWorkspaceDir(dir); err != nil {
		return SystemInfo{}, err
	}
	return s.Info(), nil
}

// MigrateDataDir 把数据迁到新目录：sqlite 用 VACUUM INTO 做在线一致
// 快照，转录逐文件拷贝（append-only，拷贝安全），最后写固定配置文件。
// 旧数据留在原地不动；新目录在下次启动时生效。
func (s *Service) MigrateDataDir(ctx context.Context, target string) (SystemInfo, error) {
	target = filepath.Clean(target)
	if !filepath.IsAbs(target) {
		return SystemInfo{}, fmt.Errorf("%w: data dir must be an absolute path", service.ErrInvalid)
	}
	if config.SamePath(target, s.cfg.DataDir) {
		return SystemInfo{}, fmt.Errorf("%w: already using this directory", service.ErrInvalid)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return SystemInfo{}, fmt.Errorf("create target dir: %w", err)
	}
	targetDB := filepath.Join(target, "acp.db")
	if _, err := os.Stat(targetDB); err == nil {
		// 覆盖别人的数据库比失败糟糕得多，让用户换个目录或清空它。
		return SystemInfo{}, fmt.Errorf("%w: target already contains acp.db", service.ErrInvalid)
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
