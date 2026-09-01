package datasource

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"acpp/server/internal/gitrepo"
	"acpp/server/internal/model"
)

// 扫工作区找 git 仓库时的深度上限。布局最深是
// `<工作区根>/<租户>/<组织>/<仓库>` 三层，多给一层余量。
const scanDepth = 4

// MigrateProjectNames 把数据源的项目名升级成 git 仓库的规范名
// （`pp-game` → `BDBGAME2024/pp-game`，见 README「项目」一节）。
//
// 启动时跑一次，**幂等**：已经是 `<组织>/<仓库>` 形式的不动。
//
// **只在唯一匹配时改**：工作区里同名仓库出现在两个组织下时，谁也说不准
// 用户当初指的是哪个——那时保持原样，裸名本来就还是有效候选，功能不受
// 影响，只是没升级。宁可不动，也不能替用户猜错一个库。
func (s *Service) MigrateProjectNames(ctx context.Context) error {
	var rows []model.DataSource
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return fmt.Errorf("扫描数据源: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	// 先看有没有要升级的，没有就不去扫盘。
	pending := false
	for i := range rows {
		if !strings.Contains(rows[i].Project, "/") {
			pending = true
			break
		}
	}
	if !pending {
		return nil
	}

	byBase := scanRepoNames(s.workspaceRoot())
	if len(byBase) == 0 {
		return nil
	}

	var done, skipped int
	for i := range rows {
		src := &rows[i]
		base := strings.TrimSpace(src.Project)
		if base == "" || strings.Contains(base, "/") {
			continue
		}
		full, ok := byBase[strings.ToLower(base)]
		if !ok {
			continue
		}
		if full == "" {
			// 同名仓库在多个组织下：不猜。
			slog.Info("数据源项目名有歧义，保持原样", "ref", src.Project+"/"+src.Env, "project", base)
			skipped++
			continue
		}
		if strings.EqualFold(full, base) {
			continue
		}
		if err := s.db.WithContext(ctx).Model(src).Update("project", full).Error; err != nil {
			slog.Error("升级数据源项目名失败", "id", src.ID, "from", base, "to", full, "err", err)
			skipped++
			continue
		}
		slog.Info("数据源项目名已升级为仓库规范名", "id", src.ID, "from", base, "to", full)
		done++
	}
	if done > 0 || skipped > 0 {
		slog.Info("数据源项目名迁移完成", "upgraded", done, "skipped", skipped)
	}
	return nil
}

// scanRepoNames 扫工作区，给出 `裸仓库名（小写）→ 规范名` 的映射。
// 同名仓库出现在多个组织下时值为空串——那是「有歧义，别猜」的标记。
func scanRepoNames(root string) map[string]string {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	out := map[string]string{}
	rootDepth := len(strings.Split(filepath.Clean(root), string(filepath.Separator)))

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil //nolint:nilerr // 读不了的目录跳过即可，迁移不该因此中断
		}
		name := d.Name()
		if path != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor") {
			return fs.SkipDir
		}
		if len(strings.Split(path, string(filepath.Separator)))-rootDepth > scanDepth {
			return fs.SkipDir
		}
		if _, statErr := os.Lstat(filepath.Join(path, ".git")); statErr != nil {
			return nil
		}
		// 是个仓库：登记它，并且不再往下走（子目录不是独立项目）。
		full := gitrepo.NameOfDir(path)
		if full == "" {
			return fs.SkipDir
		}
		base := strings.ToLower(filepath.Base(full))
		if prev, seen := out[base]; seen && !strings.EqualFold(prev, full) {
			out[base] = "" // 歧义
		} else if !seen {
			out[base] = full
		}
		return fs.SkipDir
	})
	return out
}
