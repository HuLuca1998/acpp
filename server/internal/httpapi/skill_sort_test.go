package httpapi

import (
	"testing"
	"time"

	"acpp/server/internal/service"
)

func names(skills []service.Skill) []string {
	out := make([]string, len(skills))
	for i, s := range skills {
		out[i] = s.Name
	}
	return out
}

// 契约：技能排序在切页**之前**做。
//
// 反过来就只排了当前这一页——用户以为看到的是「全部里用得最多的」，
// 其实是「这 20 条里用得最多的」。那种错比没有排序更坏，所以这里连同
// 各个字段的比较规则一起钉住。
func TestSortSkills(t *testing.T) {
	day := func(n int) time.Time {
		return time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC)
	}
	base := []service.Skill{
		{Name: "beta", UsageCount: 5, Enabled: true, UpdatedAt: day(2)},
		{Name: "Alpha", UsageCount: 1, Enabled: false, UpdatedAt: day(3)},
		{Name: "gamma", UsageCount: 9, Enabled: true, UpdatedAt: day(1)},
	}

	cases := []struct {
		name string
		spec SortSpec
		want []string
	}{
		{"没指定就不动，保持 List 给的顺序", SortSpec{}, []string{"beta", "Alpha", "gamma"}},
		// 大小写不敏感：技能名多是小写，混进一个大写开头的会排到最前，
		// 看起来像乱序。
		{"按名字升序（大小写不敏感）", SortSpec{Column: "name"}, []string{"Alpha", "beta", "gamma"}},
		{"按名字降序", SortSpec{Column: "name", Desc: true}, []string{"gamma", "beta", "Alpha"}},
		{"按用量升序", SortSpec{Column: "usage_count"}, []string{"Alpha", "beta", "gamma"}},
		{"按用量降序", SortSpec{Column: "usage_count", Desc: true}, []string{"gamma", "beta", "Alpha"}},
		{"按更新时间升序", SortSpec{Column: "updated_at"}, []string{"gamma", "beta", "Alpha"}},
		// 停用的排前面（false < true），同档内保持原有相对顺序。
		{"按启停升序", SortSpec{Column: "enabled"}, []string{"Alpha", "beta", "gamma"}},
		// 白名单在 handler 那层（sortParams），认不出的字段到不了这里；
		// default 分支按名字排只是兜底，不会让页面打不开。
		{"白名单外的字段兜底按名字排", SortSpec{Column: "nope"}, []string{"Alpha", "beta", "gamma"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			skills := append([]service.Skill(nil), base...)
			sortSkills(skills, c.spec)
			got := names(skills)
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("sortSkills(%+v) = %v，想要 %v", c.spec, got, c.want)
				}
			}
		})
	}
}
