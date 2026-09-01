package datasource

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"acpp/server/internal/model"
)

// mkRepo 在工作区里造一个带 origin 的仓库。
func mkRepo(t *testing.T, root, rel, origin string) {
	t.Helper()
	dir := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if origin == "" {
		return
	}
	cfg := "[remote \"origin\"]\n\turl = " + origin + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 契约：裸仓库名升级成规范名（`pp-game` → `BDBGAME2024/pp-game`），
// 已经是规范名的不动，跑第二遍什么都不做。
func TestService_MigrateProjectNames(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root, filepath.Join("orange", "BDBGAME2024", "pp-game"), "git@github.com:BDBGAME2024/pp-game.git")
	mkRepo(t, root, filepath.Join("orange", "BDBGAME2024", "onepay"), "https://github.com/BDBGAME2024/onepay")

	svc := testService(t, root)
	ctx := context.Background()
	seed := []model.DataSource{
		{Project: "pp-game", Env: "pre", Host: "h", Port: 3306, User: "u", Database: "d"},
		{Project: "pp-game", Env: "prod", Host: "h", Port: 3306, User: "u", Database: "d"},
		{Project: "onepay", Env: "prod", Host: "h", Port: 3306, User: "u", Database: "d"},
		// 已经是规范名的：不动。
		{Project: "BDBGAME2024/pp-game", Env: "local", Host: "h", Port: 3306, User: "u", Database: "d"},
		// 工作区里没有的仓库：没法升级，保持原样（裸名仍是有效候选）。
		{Project: "unknown-thing", Env: "local", Host: "h", Port: 3306, User: "u", Database: "d"},
	}
	for i := range seed {
		if err := svc.db.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	if err := svc.MigrateProjectNames(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var rows []model.DataSource
	if err := svc.db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	byEnv := map[string][]string{}
	for _, r := range rows {
		byEnv[r.Env] = append(byEnv[r.Env], r.Project)
	}
	if byEnv["pre"][0] != "BDBGAME2024/pp-game" {
		t.Errorf("pp-game 应升级成 BDBGAME2024/pp-game，得到 %q", byEnv["pre"][0])
	}
	for _, p := range byEnv["prod"] {
		if p != "BDBGAME2024/pp-game" && p != "BDBGAME2024/onepay" {
			t.Errorf("prod 的项目名没升级: %q", p)
		}
	}
	for _, p := range byEnv["local"] {
		if p != "BDBGAME2024/pp-game" && p != "unknown-thing" {
			t.Errorf("local 的项目名被改坏了: %q", p)
		}
	}

	// 幂等：再跑一遍不改变任何东西。
	before := append([]model.DataSource(nil), rows...)
	if err := svc.MigrateProjectNames(ctx); err != nil {
		t.Fatalf("migrate again: %v", err)
	}
	var after []model.DataSource
	if err := svc.db.Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	for i := range after {
		if after[i].Project != before[i].Project {
			t.Errorf("重复迁移改动了数据: %q → %q", before[i].Project, after[i].Project)
		}
	}
}

// 契约：同名仓库在多个组织下时**不猜**——保持原样，裸名仍是有效候选。
// 替用户猜错一个库，比不升级严重得多。
func TestService_MigrateProjectNames_AmbiguousLeftAlone(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root, filepath.Join("a", "org-one", "shared"), "git@github.com:org-one/shared.git")
	mkRepo(t, root, filepath.Join("b", "org-two", "shared"), "git@github.com:org-two/shared.git")

	svc := testService(t, root)
	row := model.DataSource{Project: "shared", Env: "local", Host: "h", Port: 3306, User: "u", Database: "d"}
	if err := svc.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}

	if err := svc.MigrateProjectNames(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var got model.DataSource
	if err := svc.db.First(&got, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Project != "shared" {
		t.Errorf("有歧义时不该改，得到 %q", got.Project)
	}
}
