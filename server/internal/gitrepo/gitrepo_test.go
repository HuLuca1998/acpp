package gitrepo

import "testing"

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
