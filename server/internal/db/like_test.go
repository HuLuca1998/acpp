package db

import "testing"

// 契约：用户输入里的通配符是字面量。搜「a_b」不该匹配「acb」，搜「%」不该
// 匹配全部；同时正常子串照样命中。
func TestLikePattern_EscapesWildcards(t *testing.T) {
	cases := map[string]string{
		"abc":  "%abc%",
		"a_b":  `%a\_b%`,
		"100%": `%100\%%`,
		`a\b`:  `%a\\b%`,
		" x ":  "%x%",
	}
	for in, want := range cases {
		if got := LikePattern(in); got != want {
			t.Errorf("LikePattern(%q) = %q, want %q", in, got, want)
		}
	}
}
