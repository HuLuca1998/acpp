package datasource

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// systemSchemas 是 MySQL 自带的库。不隐藏它们（排查问题时要看），
// 但标出来，免得在一堆业务库里被当成自己的。
var systemSchemas = map[string]bool{
	"information_schema": true,
	"performance_schema": true,
	"mysql":              true,
	"sys":                true,
}

// Database 是一个库的概览。
type Database struct {
	Name      string `json:"name"`
	Charset   string `json:"charset,omitempty"`
	Collation string `json:"collation,omitempty"`
	System    bool   `json:"system,omitempty"`
	Tables    int    `json:"tables"`
}

// Table 是一张表（或视图）的概览。Rows 是 information_schema 的估算值，
// InnoDB 下并不精确——要准确计数得自己 count(*)，这里只用于排序与感知量级。
type Table struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Engine  string `json:"engine,omitempty"`
	Rows    int64  `json:"rows"`
	Comment string `json:"comment,omitempty"`
}

// Column 是一列的定义。
type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Key      string `json:"key,omitempty"`
	Default  string `json:"default,omitempty"`
	Extra    string `json:"extra,omitempty"`
	Comment  string `json:"comment,omitempty"`
}

// Index 是一个索引，Columns 按索引内顺序排列。
type Index struct {
	Name    string   `json:"name"`
	Unique  bool     `json:"unique"`
	Type    string   `json:"type,omitempty"`
	Columns []string `json:"columns"`
}

// TableDetail 是表结构详情。DDL 是 SHOW CREATE TABLE 的原文——
// 给 AI 看一份完整建表语句，胜过它自己拼十次 information_schema 查询。
type TableDetail struct {
	Database string   `json:"database"`
	Name     string   `json:"name"`
	Columns  []Column `json:"columns"`
	Indexes  []Index  `json:"indexes"`
	DDL      string   `json:"ddl,omitempty"`
}

// Databases 列出数据源上的全部库，带表数量。
func Databases(ctx context.Context, src *model.DataSource) ([]Database, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	h, err := connect(ctx, src, "")
	if err != nil {
		return nil, err
	}
	defer h.Close()

	const q = `SELECT s.SCHEMA_NAME, s.DEFAULT_CHARACTER_SET_NAME, s.DEFAULT_COLLATION_NAME,
		(SELECT COUNT(*) FROM information_schema.TABLES t WHERE t.TABLE_SCHEMA = s.SCHEMA_NAME)
		FROM information_schema.SCHEMATA s ORDER BY s.SCHEMA_NAME`
	rows, err := h.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("列出数据库失败: %w", err)
	}
	defer rows.Close()

	out := []Database{}
	for rows.Next() {
		var d Database
		var charset, collation sql.NullString
		if err := rows.Scan(&d.Name, &charset, &collation, &d.Tables); err != nil {
			return nil, err
		}
		d.Charset, d.Collation = charset.String, collation.String
		d.System = systemSchemas[strings.ToLower(d.Name)]
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 收窄到数据源允许的范围：一个账号常能连到整台实例，但用户配这条
	// 连接时想的是「这个项目的库」（见 allow.go）。
	return filterDatabases(src, out), nil
}

// Tables 列出一个库里的表与视图。
func Tables(ctx context.Context, src *model.DataSource, database string) ([]Table, error) {
	if strings.TrimSpace(database) == "" {
		database = src.Database
	}
	if strings.TrimSpace(database) == "" {
		return nil, fmt.Errorf("%w: 未指定数据库", service.ErrInvalid)
	}
	if err := guardDatabase(src, database); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	h, err := connect(ctx, src, "")
	if err != nil {
		return nil, err
	}
	defer h.Close()

	const q = `SELECT TABLE_NAME, TABLE_TYPE, IFNULL(ENGINE,''), IFNULL(TABLE_ROWS,0), IFNULL(TABLE_COMMENT,'')
		FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME`
	rows, err := h.db.QueryContext(ctx, q, database)
	if err != nil {
		return nil, fmt.Errorf("列出表失败: %w", err)
	}
	defer rows.Close()

	out := []Table{}
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Name, &t.Type, &t.Engine, &t.Rows, &t.Comment); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Describe 返回一张表的列、索引与建表语句。
func Describe(ctx context.Context, src *model.DataSource, database, table string) (*TableDetail, error) {
	if strings.TrimSpace(database) == "" {
		database = src.Database
	}
	if strings.TrimSpace(database) == "" || strings.TrimSpace(table) == "" {
		return nil, fmt.Errorf("%w: 需要同时指定库与表", service.ErrInvalid)
	}
	if err := guardDatabase(src, database); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	h, err := connect(ctx, src, "")
	if err != nil {
		return nil, err
	}
	defer h.Close()

	detail := &TableDetail{Database: database, Name: table}
	if detail.Columns, err = queryColumns(ctx, h.db, database, table); err != nil {
		return nil, err
	}
	if len(detail.Columns) == 0 {
		return nil, fmt.Errorf("%w: 表 %s.%s 不存在", service.ErrNotFound, database, table)
	}
	if detail.Indexes, err = queryIndexes(ctx, h.db, database, table); err != nil {
		return nil, err
	}
	detail.DDL = queryDDL(ctx, h.db, database, table)
	return detail, nil
}

func queryColumns(ctx context.Context, db *sql.DB, database, table string) ([]Column, error) {
	const q = `SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, IFNULL(COLUMN_KEY,''),
		IFNULL(COLUMN_DEFAULT,''), IFNULL(EXTRA,''), IFNULL(COLUMN_COMMENT,'')
		FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`
	rows, err := db.QueryContext(ctx, q, database, table)
	if err != nil {
		return nil, fmt.Errorf("读表结构失败: %w", err)
	}
	defer rows.Close()

	out := []Column{}
	for rows.Next() {
		var c Column
		var nullable string
		if err := rows.Scan(&c.Name, &c.Type, &nullable, &c.Key, &c.Default, &c.Extra, &c.Comment); err != nil {
			return nil, err
		}
		c.Nullable = strings.EqualFold(nullable, "YES")
		out = append(out, c)
	}
	return out, rows.Err()
}

func queryIndexes(ctx context.Context, db *sql.DB, database, table string) ([]Index, error) {
	const q = `SELECT INDEX_NAME, NON_UNIQUE, IFNULL(INDEX_TYPE,''), COLUMN_NAME
		FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY INDEX_NAME, SEQ_IN_INDEX`
	rows, err := db.QueryContext(ctx, q, database, table)
	if err != nil {
		return nil, fmt.Errorf("读索引失败: %w", err)
	}
	defer rows.Close()

	out := []Index{}
	byName := map[string]int{}
	for rows.Next() {
		var name, indexType, column string
		var nonUnique int
		if err := rows.Scan(&name, &nonUnique, &indexType, &column); err != nil {
			return nil, err
		}
		i, ok := byName[name]
		if !ok {
			out = append(out, Index{Name: name, Unique: nonUnique == 0, Type: indexType})
			i = len(out) - 1
			byName[name] = i
		}
		out[i].Columns = append(out[i].Columns, column)
	}
	return out, rows.Err()
}

// queryDDL 取建表语句。拿不到不算失败——权限不足时 SHOW CREATE 会被拒，
// 但列与索引已经够用了，没必要让整个请求失败。
func queryDDL(ctx context.Context, db *sql.DB, database, table string) string {
	// 标识符不能用占位符，只能拼接：反引号转义后包起来，避免注入。
	q := fmt.Sprintf("SHOW CREATE TABLE %s.%s", quoteIdent(database), quoteIdent(table))
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return ""
	}
	defer rows.Close()
	if !rows.Next() {
		return ""
	}
	// SHOW CREATE TABLE 是 2 列，视图是 4 列（多 character_set_client 等）。
	cols, err := rows.Columns()
	if err != nil {
		return ""
	}
	holders := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range holders {
		ptrs[i] = &holders[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return ""
	}
	if len(holders) < 2 {
		return ""
	}
	if s, ok := encodeValue(holders[1]).(string); ok {
		return s
	}
	return ""
}

func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
