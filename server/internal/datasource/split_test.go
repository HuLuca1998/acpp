package datasource

import (
	"reflect"
	"testing"
)

// 契约：脚本按分隔符切成多条语句，且分号出现在字符串/标识符/注释里时
// 不算边界——切错一刀，用户看到的就是「你把我的 SQL 拆坏了」。
func TestSplit(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   []string
	}{
		{
			name:   "单条无分号",
			script: "SELECT 1",
			want:   []string{"SELECT 1"},
		},
		{
			name:   "多条与空语句",
			script: "SELECT 1; SELECT 2;;\n\nSELECT 3;",
			want:   []string{"SELECT 1", "SELECT 2", "SELECT 3"},
		},
		{
			name:   "字符串里的分号不切",
			script: `INSERT INTO t VALUES ('a;b'); SELECT 1`,
			want:   []string{`INSERT INTO t VALUES ('a;b')`, "SELECT 1"},
		},
		{
			name:   "反引号标识符里的分号不切",
			script: "SELECT `we;ird` FROM t; SELECT 2",
			want:   []string{"SELECT `we;ird` FROM t", "SELECT 2"},
		},
		{
			name:   "反斜杠转义的引号不结束字符串",
			script: `SELECT 'it\'s; here'; SELECT 2`,
			want:   []string{`SELECT 'it\'s; here'`, "SELECT 2"},
		},
		{
			name:   "叠写引号不结束字符串",
			script: `SELECT 'it''s; here'; SELECT 2`,
			want:   []string{`SELECT 'it''s; here'`, "SELECT 2"},
		},
		{
			name:   "行注释整行丢弃",
			script: "-- drop; everything\nSELECT 1; # tail; comment\nSELECT 2",
			want:   []string{"SELECT 1", "SELECT 2"},
		},
		{
			name:   "两个减号后面不是空白时不是注释",
			script: "SELECT 1--2; SELECT 3",
			want:   []string{"SELECT 1--2", "SELECT 3"},
		},
		{
			name:   "块注释丢弃但不粘连 token",
			script: "SELECT/* a;b */1; SELECT 2",
			want:   []string{"SELECT 1", "SELECT 2"},
		},
		{
			name:   "版本化注释保留执行",
			script: "/*!40101 SET NAMES utf8 */; SELECT 1",
			want:   []string{"/*!40101 SET NAMES utf8 */", "SELECT 1"},
		},
		{
			name: "DELIMITER 让函数体里的分号不再是边界",
			script: "DELIMITER $$\n" +
				"CREATE PROCEDURE p() BEGIN SELECT 1; SELECT 2; END$$\n" +
				"DELIMITER ;\n" +
				"SELECT 3;",
			want: []string{
				"CREATE PROCEDURE p() BEGIN SELECT 1; SELECT 2; END",
				"SELECT 3",
			},
		},
		{
			name:   "只有注释与空白时没有语句",
			script: "-- nothing here\n\n  ",
			want:   nil,
		},
		{
			name:   "未闭合的引号原样交给数据库报错",
			script: "SELECT 'unterminated",
			want:   []string{"SELECT 'unterminated"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Split(tt.script)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Split(%q)\n got = %#v\nwant = %#v", tt.script, got, tt.want)
			}
		})
	}
}

// 契约：语句类型决定走 Query 还是 Exec——判错就会丢掉影响行数
// 或者拿不到结果集。
func TestReturnsRows(t *testing.T) {
	tests := map[string]bool{
		"SELECT 1":                             true,
		"  select * from t":                    true,
		"SHOW TABLES":                          true,
		"desc users":                           true,
		"EXPLAIN SELECT 1":                     true,
		"WITH x AS (SELECT 1) SELECT * FROM x": true,
		"(SELECT 1) UNION (SELECT 2)":          true,
		"/* hint */ SELECT 1":                  true,
		"INSERT INTO t VALUES (1)":             false,
		"update t set a=1":                     false,
		"DELETE FROM t":                        false,
		"CREATE TABLE t (id INT)":              false,
		"USE mydb":                             false,
		"SET @x = 1":                           false,
	}
	for stmt, want := range tests {
		if got := returnsRows(stmt); got != want {
			t.Errorf("returnsRows(%q) = %v, want %v", stmt, got, want)
		}
	}
}
