package httpapi

import (
	"testing"

	"acpp/server/internal/service"
)

// 契约：关键词同时看名字与描述、不分大小写；enabled 三态；两者叠加取交集；
// 都不给时原样返回。
func TestFilterSkills(t *testing.T) {
	on, off := true, false
	skills := []service.Skill{
		{Name: "db-query", Description: "查 MySQL", Enabled: true},
		{Name: "html-report", Description: "生成 HTML 报告", Enabled: false},
		{Name: "my-issues", Description: "GitHub issues", Enabled: true},
	}
	names := func(in []service.Skill) []string {
		out := make([]string, 0, len(in))
		for _, s := range in {
			out = append(out, s.Name)
		}
		return out
	}
	cases := []struct {
		kw      string
		enabled *bool
		want    []string
	}{
		{"", nil, []string{"db-query", "html-report", "my-issues"}},
		{"mysql", nil, []string{"db-query"}},
		{"HTML", nil, []string{"html-report"}},
		{"", &off, []string{"html-report"}},
		{"issues", &on, []string{"my-issues"}},
		{"html", &on, []string{}},
	}
	for _, c := range cases {
		got := names(filterSkills(skills, c.kw, c.enabled))
		if len(got) != len(c.want) {
			t.Errorf("filterSkills(%q, %v) = %v, want %v", c.kw, c.enabled, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("filterSkills(%q, %v) = %v, want %v", c.kw, c.enabled, got, c.want)
			}
		}
	}
}
