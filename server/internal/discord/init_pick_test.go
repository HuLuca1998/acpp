package discord

import (
	"strings"
	"testing"
)

// 契约：三张选择卡都在下拉下面单独一行挂刷新按钮（String Select 独占
// action row，按钮塞不进同一行），且刷新按钮的 custom_id **不能**被下拉
// 那条分发规则前缀匹配——两者同为 type 3 的组件交互，混了就会把「刷新」
// 当成「选好了」，卡片直接进入下一步。
func TestPickCardsCarryRefreshButton(t *testing.T) {
	in := initInput{repo: "org/app", defaultBranch: "main", dbRef: "proj/prod", dbID: 3}
	cases := []struct {
		label     string
		body      map[string]any
		selPrefix string
		refPrefix string
	}{
		{
			"分支", branchCard(in, "abc", "main", []string{"main", "dev"}, false),
			"br:", pickBranchRefresh + ":",
		},
		{
			"数据库", dbCard(in, "abc", []DBOption{{ID: 3, Ref: "proj/prod", Database: "app"}}, 3, false),
			"db:", pickDBRefresh + ":",
		},
		{
			"服务器", serverCard(in, "abc", []ServerOption{{ID: 1, Name: "prod-1", Host: "10.0.0.1"}}, 1, false),
			"srv:", pickServerRefresh + ":",
		},
	}
	for _, tc := range cases {
		rows, ok := tc.body["components"].([]map[string]any)
		if !ok || len(rows) != 2 {
			t.Errorf("%s: 组件行 = %v，want 2 行（下拉一行 + 按钮一行）", tc.label, tc.body["components"])
			continue
		}
		sel := rows[0]["components"].([]map[string]any)[0]
		if got := sel["custom_id"]; !strings.HasPrefix(got.(string), tc.selPrefix) {
			t.Errorf("%s: 下拉 custom_id = %v, want 前缀 %q", tc.label, got, tc.selPrefix)
		}
		btn := rows[1]["components"].([]map[string]any)[0]
		if btn["type"] != 2 {
			t.Errorf("%s: 第二行不是按钮：%v", tc.label, btn)
			continue
		}
		id, _ := btn["custom_id"].(string)
		if !strings.HasPrefix(id, tc.refPrefix) {
			t.Errorf("%s: 刷新 custom_id = %q, want 前缀 %q", tc.label, id, tc.refPrefix)
		}
		if strings.HasPrefix(id, tc.selPrefix) {
			t.Errorf("%s: 刷新 custom_id %q 会被下拉分发吃掉", tc.label, id)
		}
	}
}

// 契约：刷新过的卡片在描述里留下痕迹。清单内容前后往往一模一样，
// 没有这句话用户按了按钮也不知道生效没有。
func TestRefreshedCardNotesTheRefresh(t *testing.T) {
	in := initInput{repo: "org/app", defaultBranch: "main"}
	plain := branchCard(in, "abc", "main", []string{"main", "dev"}, false)
	refreshed := branchCard(in, "abc", "main", []string{"main", "dev"}, true)

	desc := func(body map[string]any) string {
		return body["embeds"].([]map[string]any)[0]["description"].(string)
	}
	if strings.Contains(desc(plain), "刚刷新") {
		t.Errorf("首次渲染不该带刷新标记：%q", desc(plain))
	}
	if !strings.Contains(desc(refreshed), "刚刷新") {
		t.Errorf("刷新后应带刷新标记：%q", desc(refreshed))
	}
}
