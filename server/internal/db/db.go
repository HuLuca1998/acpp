package db

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"acpp/server/internal/config"
	"acpp/server/internal/model"
)

// Open 打开 sqlite 并执行迁移。DSN 所在目录会被自动创建。
func Open(cfg config.Config) (*gorm.DB, error) {
	if dir := filepath.Dir(cfg.DSN); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
	}

	level := logger.Warn
	if cfg.Debug {
		level = logger.Info
	}

	gdb, err := gorm.Open(sqlite.Open(cfg.DSN), &gorm.Config{
		Logger: logger.Default.LogMode(level),
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", cfg.DSN, err)
	}

	// sqlite 默认关闭外键约束，级联删除需要显式打开。
	if err := gdb.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if err := Migrate(gdb); err != nil {
		return nil, err
	}

	return gdb, nil
}

// Migrate 建表并补齐字段。
func Migrate(gdb *gorm.DB) error {
	// messages 表已退役（adr-003）：消息只写转录 JSONL，Message 仅作重建 DTO。
	// 旧库里已存在的表不动，这里不再创建。
	if err := gdb.AutoMigrate(&model.Agent{}, &model.Session{}, &model.SkillUsage{},
		&model.Role{}, &model.OrchSession{}, &model.OrchTask{}, &model.Tenant{},
		&model.DataSource{}); err != nil {
		return fmt.Errorf("automigrate: %w", err)
	}
	return nil
}
