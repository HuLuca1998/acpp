package datasource

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

const (
	// defaultMaxRows 是单条语句返回的行数上限。给人看的面板翻不了几千行，
	// 给 AI 看更要克制——一次 select * 就能把上下文塞满。
	defaultMaxRows = 500
	// maxRowsHard 是谁都突破不了的硬顶。调用方能调小不能调大：一条
	// `select * from 大表` 在千万行的表上能把库的连接、网络和本进程内存
	// 一起拖垮，而那从来不是任何人真正想要的结果。
	maxRowsHard = 1000
)

// StatementResult 是一条语句的执行结果。查询类填 Columns/Rows，
// 写入类填 Affected/LastInsertID，失败则只填 Error——一条语句失败不影响
// 已经执行成功的前几条，它们的结果照常返回。
type StatementResult struct {
	Statement string `json:"statement"`
	// Kind 是 query（有结果集）或 exec（只有影响行数）。
	Kind         string     `json:"kind"`
	Columns      []string   `json:"columns,omitempty"`
	Rows         [][]any    `json:"rows,omitempty"`
	RowCount     int        `json:"rowCount"`
	Truncated    bool       `json:"truncated,omitempty"`
	Affected     int64      `json:"affected,omitempty"`
	LastInsertID int64      `json:"lastInsertId,omitempty"`
	ElapsedMS    int64      `json:"elapsedMs"`
	Error        string     `json:"error,omitempty"`
	StartedAt    *time.Time `json:"-"`
}

// ExecResult 是一次执行请求的整体结果。
type ExecResult struct {
	Database  string            `json:"database"`
	Results   []StatementResult `json:"results"`
	ElapsedMS int64             `json:"elapsedMs"`
}

// Execute 在一条一次性连接上按顺序跑完脚本里的每条语句。
//
// 遇错即停（与 mysql 客户端默认行为一致）：前面成功的结果照常返回，
// 出错那条带上错误文本，后面的不执行——半截脚本继续跑下去只会把数据
// 搞得更乱。全部语句共用同一条物理连接，所以事务、临时表、会话变量
// 跨语句有效。
// allowWrite 表示这次调用是否允许改数据：查询通道（db_query、界面的查询）
// 传 false，执行通道（db_execute）传 `!src.ReadOnly`。
func Execute(ctx context.Context, src *model.DataSource, database, script string,
	maxRows int, allowWrite bool) (*ExecResult, error) {
	stmts := Split(script)
	if len(stmts) == 0 {
		return nil, fmt.Errorf("%w: 没有可执行的语句", service.ErrInvalid)
	}
	if maxRows <= 0 {
		maxRows = defaultMaxRows
	}
	maxRows = min(maxRows, maxRowsHard)

	// 库级范围：指定的库要在范围内，语句里也不能明写范围外的库
	//（收窄视野而非安全边界，边界说明见 allow.go）。
	if err := guardDatabase(src, database); err != nil {
		return nil, err
	}
	if err := guardStatements(src, stmts); err != nil {
		return nil, err
	}
	if err := guardWrites(src, stmts, allowWrite); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	h, err := connect(ctx, src, database)
	if err != nil {
		return nil, err
	}
	defer h.Close()

	out := &ExecResult{Database: firstNonEmpty(database, src.Database)}
	total := time.Now()
	for _, stmt := range stmts {
		res := runStatement(ctx, h.db, stmt, maxRows)
		out.Results = append(out.Results, res)
		if res.Error != "" {
			break
		}
	}
	out.ElapsedMS = time.Since(total).Milliseconds()
	return out, nil
}

func runStatement(ctx context.Context, db *sql.DB, stmt string, maxRows int) StatementResult {
	res := StatementResult{Statement: stmt, Kind: "exec"}
	started := time.Now()
	defer func() { res.ElapsedMS = time.Since(started).Milliseconds() }()

	if returnsRows(stmt) {
		res.Kind = "query"
		// 语句原样下发——不给用户的 SQL 自动加 LIMIT，也不在库上设任何
		// 会话变量。护栏全部落在我们这侧：读够行就取消这次查询。
		//
		// 取消而不是只 break，是因为 rows.Close() 会先把剩余结果读干净；
		// 一条命中千万行的 select 光是「读完再丢掉」就够把网络和内存占满。
		// cancel 让驱动真的中断这次查询。
		queryCtx, cancelQuery := context.WithCancel(ctx)
		defer cancelQuery()

		rows, err := db.QueryContext(queryCtx, stmt)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		defer rows.Close()
		if err := scanRows(rows, &res, maxRows); err != nil {
			res.Error = err.Error()
		}
		if res.Truncated {
			cancelQuery()
		}
		return res
	}

	r, err := db.ExecContext(ctx, stmt)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if n, err := r.RowsAffected(); err == nil {
		res.Affected = n
	}
	if id, err := r.LastInsertId(); err == nil {
		res.LastInsertID = id
	}
	return res
}

func scanRows(rows *sql.Rows, res *StatementResult, maxRows int) error {
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	res.Columns = cols

	holders := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range holders {
		ptrs[i] = &holders[i]
	}

	for rows.Next() {
		if res.RowCount >= maxRows {
			res.Truncated = true
			break
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		row := make([]any, len(cols))
		for i, v := range holders {
			row[i] = encodeValue(v)
		}
		res.Rows = append(res.Rows, row)
		res.RowCount++
	}
	return rows.Err()
}

// encodeValue 把驱动给的原始值转成能安全进 JSON 的形态。
// MySQL 驱动几乎所有列都给 []byte：能当 UTF-8 读就当字符串，
// 真二进制（BLOB）转 base64 并加前缀标出来，不要让乱码进上下文。
func encodeValue(v any) any {
	b, ok := v.([]byte)
	if !ok {
		return v
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return "base64:" + base64.StdEncoding.EncodeToString(b)
}

// returnsRows 判断一条语句该走 Query 还是 Exec。
//
// 靠首个关键字判断而不是「先 Query 再看有没有列」：Exec 才拿得到
// 影响行数与自增 id，用 Query 跑 INSERT 会把这两个最有用的反馈丢掉。
func returnsRows(stmt string) bool {
	head := strings.ToLower(firstWord(stmt))
	switch head {
	case "select", "show", "desc", "describe", "explain", "analyze",
		"with", "table", "values", "call", "check", "help":
		return true
	}
	return false
}

// firstWord 取语句的第一个关键字，跳过前导注释与括号
// （`(SELECT ...) UNION ...` 与 `/* hint */ SELECT` 都要能认出来）。
func firstWord(stmt string) string {
	s := strings.TrimLeft(stmt, " \t\r\n(")
	for strings.HasPrefix(s, "/*") {
		end := strings.Index(s, "*/")
		if end < 0 {
			return ""
		}
		s = strings.TrimLeft(s[end+2:], " \t\r\n(")
	}
	end := strings.IndexFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '(' || r == ';'
	})
	if end < 0 {
		return s
	}
	return s[:end]
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
