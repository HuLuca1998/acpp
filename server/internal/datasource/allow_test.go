package datasource

import (
	"errors"
	"testing"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// 契约：一条连接只对应一个库，指定别的库一律拒绝。
//
// 这是整个功能里最要紧的一条约束——跨过去就意味着一条为 A 库配的连接
// 能读写 B 库。宁可测得啰嗦。
func TestGuardDatabase(t *testing.T) {
	src := &model.DataSource{Ref: "pp-game/dev", Database: "pp_game"}

	// 空 = 用这条连接的库；大小写不敏感（MySQL 的库名大小写规则随平台变）。
	for _, ok := range []string{"", "pp_game", "PP_GAME", "  pp_game  "} {
		if err := guardDatabase(src, ok); err != nil {
			t.Errorf("guardDatabase(%q) 应放行: %v", ok, err)
		}
	}

	for _, bad := range []string{"other_app", "mysql", "information_schema", "pp_game2", "pp"} {
		if err := guardDatabase(src, bad); !errors.Is(err, service.ErrForbidden) {
			t.Errorf("guardDatabase(%q) 应报 ErrForbidden，实际 %v", bad, err)
		}
	}
}

// 契约：语句里明写别的库要被拒。
//
// 这是**闸门不是边界**——只挡明写的限定名，动态 SQL 绕得过去。测试固定住
// 能挡的那部分，也固定住不该误伤的那部分：误伤比漏判更让人没法干活。
func TestGuardStatements(t *testing.T) {
	src := &model.DataSource{Ref: "pp-game/dev", Database: "pp_game"}

	blocked := []string{
		"SELECT * FROM other_db.users",
		"select a.* from pp_game.t a join secret.t b on a.id=b.id",
		"INSERT INTO other_db.log VALUES (1)",
		"UPDATE other_db.t SET a=1",
		"DELETE FROM other_db.t",
		"SELECT * FROM `other-db`.users",   // 反引号引用的库名
		"SELECT * FROM `other_db` . users", // 中间有空格
		"select * from MYSQL.user",         // 系统库同样挡
	}
	for _, stmt := range blocked {
		if err := guardStatements(src, []string{stmt}); !errors.Is(err, service.ErrForbidden) {
			t.Errorf("应拒绝跨库语句 %q，实际 %v", stmt, err)
		}
	}

	allowed := []string{
		"SELECT * FROM users",         // 不带库限定
		"SELECT * FROM pp_game.users", // 就是绑定的库
		"SELECT * FROM PP_GAME.users", // 大小写不敏感
		"SELECT u.id, o.amount FROM users u JOIN orders o ON o.user_id=u.id",
		"SELECT t.name FROM pp_game.t AS t",             // 表别名限定列，不是库
		"SELECT * FROM t WHERE note = 'other_db.users'", // 字符串里的不算引用
		"SELECT * FROM t -- other_db.users",             // 注释里的不算
		"SELECT * FROM t /* other_db.users */",          // 块注释同理
		"SELECT 1.5 AS x",                               // 小数点不是库限定
	}
	for _, stmt := range allowed {
		if err := guardStatements(src, []string{stmt}); err != nil {
			t.Errorf("不该拒绝 %q: %v", stmt, err)
		}
	}

	// 多条语句里只要有一条越界，整批都不执行。
	batch := []string{"SELECT * FROM users", "SELECT * FROM other_db.t"}
	if err := guardStatements(src, batch); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("批次里有越界语句应整体拒绝，实际 %v", err)
	}
}

// 契约：只读连接拒绝写语句；查询通道即使连接可写也只跑只读语句。
func TestGuardWrites(t *testing.T) {
	readOnly := &model.DataSource{Ref: "pp-game/prod", Database: "pp_game", ReadOnly: true}
	writable := &model.DataSource{Ref: "pp-game/dev", Database: "pp_game"}

	reads := []string{
		"SELECT 1",
		"show tables",
		"DESC users",
		"EXPLAIN SELECT 1",
		"WITH x AS (SELECT 1) SELECT * FROM x",
	}
	writes := []string{
		"INSERT INTO t VALUES (1)",
		"update t set a=1",
		"DELETE FROM t",
		"DROP TABLE t",
		"TRUNCATE t",
		"CREATE TABLE t (id INT)",
		"CALL some_proc()", // 存储过程从调用点看不出会不会写，按写处理
		"WITH x AS (SELECT 1) DELETE FROM t WHERE id IN (SELECT id FROM x)",
	}

	// 查询通道（allowWrite=false）：读放行、写拒绝。
	for _, stmt := range reads {
		if err := guardWrites(writable, []string{stmt}, false); err != nil {
			t.Errorf("查询通道不该拒绝 %q: %v", stmt, err)
		}
	}
	for _, stmt := range writes {
		if err := guardWrites(writable, []string{stmt}, false); err == nil {
			t.Errorf("查询通道应拒绝写语句 %q", stmt)
		}
		// 只读连接上的写语句是 ErrForbidden（配置说了不许），
		// 可写连接走查询通道是 ErrInvalid（用错了通道）。
		if err := guardWrites(readOnly, []string{stmt}, false); !errors.Is(err, service.ErrForbidden) {
			t.Errorf("只读连接应报 ErrForbidden（%q）: %v", stmt, err)
		}
	}

	// 执行通道（allowWrite=true）：写语句放行。
	for _, stmt := range writes {
		if err := guardWrites(writable, []string{stmt}, true); err != nil {
			t.Errorf("执行通道不该拒绝 %q: %v", stmt, err)
		}
	}
}
