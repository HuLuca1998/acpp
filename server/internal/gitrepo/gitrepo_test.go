package gitrepo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidCloneURL(t *testing.T) {
	cases := []struct {
		url string
		ok  bool
	}{
		{"https://github.com/org/app.git", true},
		{"https://github.com/org/app", true},
		{"git@github.com:org/app.git", true},
		{"file:///etc/passwd", false},
		{"ext::sh -c whoami", false},
		{"ssh://git@github.com/org/app", false},
		{"", false},
	}
	for _, c := range cases {
		if got := ValidCloneURL(c.url); got != c.ok {
			t.Errorf("ValidCloneURL(%q) = %v, want %v", c.url, got, c.ok)
		}
	}
}

func TestName(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://github.com/BDBGAME2024/pp-game.git", "BDBGAME2024/pp-game"},
		{"https://github.com/BDBGAME2024/pp-game/", "BDBGAME2024/pp-game"},
		{"git@github.com:org/app.git", "org/app"},
		{"https://gitlab.example.com/group/sub/app.git", "sub/app"},
		{"app", "app"},
	}
	for _, c := range cases {
		if got := Name(c.url); got != c.want {
			t.Errorf("Name(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

// 契约：**项目就是一个 git 仓库**——同一个仓库克隆到哪儿都是同一个项目。
// 这是「项目」这个概念在本软件里的定义，数据源归属、会话归属都认它。
func TestProjectOf(t *testing.T) {
	root := t.TempDir()

	mkRepo := func(rel, origin string) string {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		if origin != "" {
			cfg := "[remote \"origin\"]\n\turl = " + origin + "\n"
			if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(cfg), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}

	t.Run("同一远端克隆到不同位置是同一个项目", func(t *testing.T) {
		a := mkRepo(filepath.Join("orange", "BDBGAME2024", "pp-game"), "git@github.com:BDBGAME2024/pp-game.git")
		b := mkRepo(filepath.Join("someone-else", "my-copy"), "https://github.com/BDBGAME2024/pp-game")
		if got, want := ProjectOf(a), "BDBGAME2024/pp-game"; got != want {
			t.Errorf("ProjectOf(租户目录下) = %q, want %q", got, want)
		}
		if got, want := ProjectOf(b), "BDBGAME2024/pp-game"; got != want {
			t.Errorf("ProjectOf(改了名的克隆) = %q, want %q", got, want)
		}
	})

	t.Run("子目录归属它所在的仓库", func(t *testing.T) {
		repo := mkRepo("solo", "git@github.com:org/solo.git")
		sub := filepath.Join(repo, "server", "internal")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if got, want := ProjectOf(sub), "org/solo"; got != want {
			t.Errorf("ProjectOf(子目录) = %q, want %q", got, want)
		}
	})

	t.Run("没有远端时退回目录名", func(t *testing.T) {
		local := mkRepo("scratch", "")
		if got, want := ProjectOf(local), "scratch"; got != want {
			t.Errorf("ProjectOf(无远端) = %q, want %q", got, want)
		}
	})

	t.Run("不是仓库就不属于任何项目", func(t *testing.T) {
		plain := filepath.Join(root, "just-a-dir")
		if err := os.MkdirAll(plain, 0o755); err != nil {
			t.Fatal(err)
		}
		// 注意：往上找会碰到 root 之外的目录，所以这里断言的是「不会拿
		// 目录名硬凑」——真找到了仓库是对的（临时目录可能在某个仓库里）。
		if got := ProjectOf(plain); got == "just-a-dir" {
			t.Errorf("不是仓库不该拿目录名当项目: %q", got)
		}
	})
}
