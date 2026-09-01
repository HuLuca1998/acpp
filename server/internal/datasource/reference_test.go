package datasource

import (
	"strings"
	"testing"

	"acpp/server/internal/model"
)

// srcs 造一组数据源，Ref 按 decorate 的规则填好。
func srcs(pairs ...[2]string) []model.DataSource {
	out := make([]model.DataSource, 0, len(pairs))
	for _, p := range pairs {
		s := model.DataSource{Project: p[0], Env: p[1], Database: "pp_game"}
		s.Ref = s.Project + "/" + s.Env
		out = append(out, s)
	}
	return out
}

// 契约：**项目名可以含斜杠**（`<组织>/<仓库>`），所以引用里数据源那部分
// 的段数不固定——靠已知清单做最长前缀匹配，剩下的才是表名。
//
// 这条以前是写死的「一段或两段」，项目名一带组织就整个解析错位。
func TestSplitRef_ProjectWithSlash(t *testing.T) {
	list := srcs(
		[2]string{"BDBGAME2024/pp-game", "pre"},
		[2]string{"BDBGAME2024/pp-game", "prod"},
		[2]string{"acpp-demo", "local"},
	)

	tests := []struct {
		name, raw, wantRef, wantTable string
	}{
		{"完整 ref", "BDBGAME2024/pp-game/pre", "BDBGAME2024/pp-game/pre", ""},
		{"完整 ref + 表", "BDBGAME2024/pp-game/pre/b_agents", "BDBGAME2024/pp-game/pre", "b_agents"},
		{"只给环境名", "prod", "BDBGAME2024/pp-game/prod", ""},
		{"环境名 + 表", "prod/b_agents", "BDBGAME2024/pp-game/prod", "b_agents"},
		{"旧写法带库名", "BDBGAME2024/pp-game/pre/pp_game/b_agents", "BDBGAME2024/pp-game/pre", "b_agents"},
		{"不含斜杠的项目", "acpp-demo/local", "acpp-demo/local", ""},
	}
	for _, tt := range tests {
		src, table, err := splitRef(list, tt.raw)
		if err != nil {
			t.Errorf("%s: splitRef(%q) 报错: %v", tt.name, tt.raw, err)
			continue
		}
		if src.Ref != tt.wantRef {
			t.Errorf("%s: 数据源 = %q, want %q", tt.name, src.Ref, tt.wantRef)
		}
		if table != tt.wantTable {
			t.Errorf("%s: 表 = %q, want %q", tt.name, table, tt.wantTable)
		}
	}
}

// 契约：认不出来时报错要带上可用清单，模型据此重试而不是瞎猜。
func TestSplitRef_NotFound(t *testing.T) {
	list := srcs([2]string{"BDBGAME2024/pp-game", "pre"})
	_, _, err := splitRef(list, "nope/nothing")
	if err == nil {
		t.Fatal("认不出的引用必须报错")
	}
	if !strings.Contains(err.Error(), "BDBGAME2024/pp-game/pre") {
		t.Errorf("报错要列出可用数据源: %v", err)
	}
}

// 契约：长的优先。`pre/users` 里 `pre` 本身是环境名，但若恰好有个数据源的
// ref 就叫 `pre/users`，那它才是用户指的那个——写得越具体越算数。
func TestSplitRef_LongestWins(t *testing.T) {
	list := srcs(
		[2]string{"pre", "users"},
		[2]string{"BDBGAME2024/pp-game", "pre"},
	)
	src, table, err := splitRef(list, "pre/users")
	if err != nil {
		t.Fatalf("splitRef: %v", err)
	}
	if src.Ref != "pre/users" || table != "" {
		t.Errorf("应匹配最长前缀 pre/users，得到 ref=%q table=%q", src.Ref, table)
	}
}
