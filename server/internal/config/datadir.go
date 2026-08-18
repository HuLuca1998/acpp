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
	// WorkspaceDir 是 agent 干活的地方（会话 cwd 的默认落点、租户 root 的
	// 父目录）。与 DataDir 刻意分开：数据目录装 db、转录与技能包，让 agent
	// 拿它当工作目录等于请它往自家数据里乱写。
	WorkspaceDir string `json:"workspaceDir,omitempty"`
	// TitleModel 是生成会话标题用的外部小模型。放本机配置而不入库：它描述
	// 的是「这台机器上有什么可用」，与租户和数据目录都无关。
	TitleModel TitleModel `json:"titleModel,omitempty"`
}

// TitleModel 是会话标题生成的模型配置，字段与 internal/titler.Config 对齐。
// 两边各存一份而不互相 import：config 与 titler 都是叶子包，桥接由装配层
// （cmd/server）做，免得为一个三字段结构在叶子之间连出依赖。
type TitleModel struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"baseUrl,omitempty"`
	Model   string `json:"model,omitempty"`
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

// DefaultWorkspaceDir 是工作区根的默认值：~/acpp。
func DefaultWorkspaceDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/acpp"
	}
	return filepath.Join(home, "acpp")
}

// SavedDataDir 读用户在设置面板选定的数据目录；没改过返回空串。
func SavedDataDir() string {
	return readFileConfig().DataDir
}

// SavedWorkspaceDir 读用户选定的工作区根；没设过返回空串。
func SavedWorkspaceDir() string {
	return readFileConfig().WorkspaceDir
}

// SaveDataDir 把选定的数据目录写进固定配置文件（迁移时调用），
// 下次启动生效。工作区根不动。
func SaveDataDir(dir string) error {
	fc := readFileConfig()
	fc.DataDir = dir
	return writeFileConfig(fc)
}

// SaveWorkspaceDir 把选定的工作区根写进配置文件。与数据目录不同，它
// **立刻生效**——不涉及已打开的数据库，只影响之后新建会话的默认落点。
func SaveWorkspaceDir(dir string) error {
	fc := readFileConfig()
	fc.WorkspaceDir = dir
	return writeFileConfig(fc)
}

// SavedTitleModel 读标题模型配置；没配过返回零值（Enabled=false 即关闭）。
func SavedTitleModel() TitleModel {
	return readFileConfig().TitleModel
}

// SaveTitleModel 保存标题模型配置，立刻生效（只影响之后新建的标题）。
func SaveTitleModel(tm TitleModel) error {
	fc := readFileConfig()
	fc.TitleModel = tm
	return writeFileConfig(fc)
}

func readFileConfig() fileConfig {
	var fc fileConfig
	raw, err := os.ReadFile(configFile())
	if err != nil {
		return fc
	}
	if err := json.Unmarshal(raw, &fc); err != nil {
		return fileConfig{}
	}
	return fc
}

func writeFileConfig(fc fileConfig) error {
	if err := os.MkdirAll(ConfigHome(), 0o755); err != nil {
		return fmt.Errorf("create config home: %w", err)
	}
	b, err := json.MarshalIndent(fc, "", "  ")
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
