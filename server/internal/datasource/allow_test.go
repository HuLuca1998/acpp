package datasource

import (
	"errors"
	"reflect"
	"testing"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// 契约：库级范围的解析。配了默认库就只看那一个是刻意的默认——一个账号
// 常能连到整台实例，用户配连接时想的是「这个项目的库」。
func TestAllowedDatabases(t *testing.T) {
	tests := []struct {
		name string
		src  model.DataSource
		want []string
	}{
		{
			name: "都没配：不限",
			src:  model.DataSource{},
			want: nil,
		},
		{
			name: "只配默认库：范围就是它",
			src:  model.DataSource{Database: "acpp_demo"},
			want: []string{"acpp_demo"},
		},
		{
			name: "显式列出多个",
			src:  model.DataSource{Database: "a", Databases: "a, b ,c"},
			want: []string{"a", "b", "c"},
		},
		{
			name: "星号显式放开（压过默认库）",
			src:  model.DataSource{Database: "acpp_demo", Databases: "*"},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := allowedDatabases(&tt.src); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("allowedDatabases = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// 契约：范围外的库一律拒绝，错误是 ErrForbidden（HTTP 403）。
func TestGuardDatabase(t *testing.T) {
	src := &model.DataSource{Ref: "pp-game/dev", Database: "pp_game"}

	for _, ok := range []string{"pp_game", "PP_GAME", ""} {
		if err := guardDatabase(src, ok); err != nil {
			t.Errorf("guardDatabase(%q) 应放行: %v", ok, err)
		}
	}
	err := guardDatabase(src, "other_app")
	if !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("范围外的库应报 ErrForbidden，实际 %v", err)
	}

	// 不限范围时什么都放行。
	if err := guardDatabase(&model.DataSource{}, "anything"); err != nil {
		t.Fatalf("未限范围应放行: %v", err)
	}
}

// 契约：语句里明写范围外的库名要被拒。
//
// 这是**收窄视野不是安全边界**——只挡明写的限定名，动态 SQL 绕得过去。
// 测试固定住能挡住的那部分，也固定住不该误伤的那部分。
func TestGuardStatements(t *testing.T) {
	src := &model.DataSource{Ref: "pp-game/dev", Databases: "pp_game, shared"}

	blocked := []string{
		"SELECT * FROM other_db.users",
		"select a.* from pp_game.t a join secret.t b on a.id=b.id",
		"INSERT INTO other_db.log VALUES (1)",
		"SELECT * FROM `other-db`.users",
	}
	for _, stmt := range blocked {
		if err := guardStatements(src, []string{stmt}); !errors.Is(err, service.ErrForbidden) {
			t.Errorf("应拒绝跨库语句 %q，实际 %v", stmt, err)
		}
	}

	allowed := []string{
		"SELECT * FROM users",                           // 不带库限定
		"SELECT * FROM pp_game.users",                   // 范围内
		"SELECT * FROM shared.dict JOIN pp_game.t ON 1", // 都在范围内
		"SELECT * FROM t WHERE note = 'other_db.users'", // 字符串里的不算引用
		"SELECT * FROM t -- other_db.users",             // 注释里的不算
		"SELECT 1.5 AS x",                               // 小数点不是库限定
		"SELECT t.name FROM pp_game.t AS t",             // 表别名限定列
	}
	for _, stmt := range allowed {
		if err := guardStatements(src, []string{stmt}); err != nil {
			t.Errorf("不该拒绝 %q: %v", stmt, err)
		}
	}

	// 不限范围时一律放行。
	if err := guardStatements(&model.DataSource{}, blocked); err != nil {
		t.Fatalf("未限范围应放行: %v", err)
	}
}
