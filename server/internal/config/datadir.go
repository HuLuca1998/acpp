package config

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// fileConfig 是固定位置 ~/.acpp/config.json 里的持久配置。
// 数据目录本身可迁移，指向它的配置必须待在固定位置才找得到。
type fileConfig struct {
	DataDir string `json:"dataDir,omitempty"`
}

// ConfigHome 是固定的配置根（也是数据目录的默认值）：~/.acpp。
// 拿不到家目录时回退旧默认 "data"（相对运行目录），行为与历史版本一致。
func ConfigHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "data"
	}
	return filepath.Join(home, ".acpp")
}

func configFile() string { return filepath.Join(ConfigHome(), "config.json") }

// SavedDataDir 读用户在设置面板选定的数据目录；没改过返回空串。
func SavedDataDir() string {
	raw, err := os.ReadFile(configFile())
	if err != nil {
		return ""
	}
	var fc fileConfig
	if err := json.Unmarshal(raw, &fc); err != nil {
		return ""
	}
	return fc.DataDir
}

// SaveDataDir 把选定的数据目录写进固定配置文件（迁移时调用），
// 下次启动生效。
func SaveDataDir(dir string) error {
	if err := os.MkdirAll(ConfigHome(), 0o755); err != nil {
		return fmt.Errorf("create config home: %w", err)
	}
	b, err := json.MarshalIndent(fileConfig{DataDir: dir}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(configFile(), b, 0o644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	return nil
}

// PrepareData 在打开数据库之前准备数据目录：创建目录，并把历史默认
// 位置（相对运行目录的 data/）的存量数据一次性拷入新目录——老用户
// 升级后无感切换到 ~/.acpp，旧数据留在原地不动。
func PrepareData(cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(cfg.DSN), 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	if err := os.MkdirAll(cfg.TranscriptDir, 0o755); err != nil {
		return fmt.Errorf("create transcript dir: %w", err)
	}

	const legacyDSN = "data/acp.db"
	if SamePath(cfg.DSN, legacyDSN) {
		return nil
	}
	if _, err := os.Stat(cfg.DSN); err == nil {
		return nil // 新库已存在，不动。
	}
	if _, err := os.Stat(legacyDSN); err != nil {
		return nil // 没有存量数据。
	}

	// 此时数据库尚未打开，冷拷贝是安全的。
	if err := copyFile(legacyDSN, cfg.DSN); err != nil {
		return fmt.Errorf("migrate legacy db: %w", err)
	}
	if err := CopyDirFiles(filepath.Join("data", "transcripts"), cfg.TranscriptDir); err != nil {
		return fmt.Errorf("migrate legacy transcripts: %w", err)
	}
	slog.Info("migrated legacy data", "from", legacyDSN, "to", cfg.DSN)
	return nil
}

// SamePath 判断两个路径解析为绝对路径后是否指向同一位置。
func SamePath(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && aa == bb
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// CopyDirFiles 把 src 下的普通文件平铺拷进 dst（转录目录就是平铺的
// jsonl，不需要递归）。src 不存在视为无事可做。
func CopyDirFiles(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := copyFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
