package mcpcall

import (
	"context"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/model"
)

func testService(t *testing.T) *Service {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(t.TempDir()+"/calls.db"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.MCPCall{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewService(gdb)
}

// 契约：超长的参数与返回在落库前被截断，且不会把半个 UTF-8 字符留在
// 末尾——一次 db_query 能拉回几百行，原样存会把库撑坏，截歪了页面上
// 就是一个乱码方块。
func TestRecordTruncatesAtRuneBoundary(t *testing.T) {
	svc := testService(t)
	// 每个汉字 3 字节，凑到远超上限，保证截断点大概率落在字符中间。
	huge := strings.Repeat("数据库查询结果", 4000)

	svc.Record(context.Background(), model.MCPCall{
		Server: "acpp-db", Tool: "db_query", Source: model.MCPSourceAgent,
		Args: huge, Result: huge,
	})

	rows, total, err := svc.List(context.Background(), Filter{}, 1, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	got := rows[0]
	if len(got.Args) >= len(huge) || len(got.Result) >= len(huge) {
		t.Fatalf("没截断: args=%d result=%d", len(got.Args), len(got.Result))
	}
	for _, s := range []string{got.Args, got.Result} {
		if !strings.HasSuffix(s, "（已截断）") {
			t.Errorf("截断没标注: %q", s[len(s)-30:])
		}
		if !isValidUTF8(s) {
			t.Error("截断切坏了 UTF-8 字符")
		}
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}

// 契约：筛选条件按「与」组合，不是随便挑一条生效。工具台的排查动作
// 就是层层收窄——筛错了等于给人看别的调用。
func TestListFilters(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	seed := []model.MCPCall{
		{Server: "acpp-db", Tool: "db_query", Source: model.MCPSourceAgent},
		{Server: "acpp-db", Tool: "db_query", Source: model.MCPSourceManual, IsError: true},
		{Server: "acpp-db", Tool: "db_execute", Source: model.MCPSourceAgent, IsError: true},
	}
	for _, rec := range seed {
		svc.Record(ctx, rec)
	}

	tests := []struct {
		name   string
		filter Filter
		want   int64
	}{
		{"不筛看全部", Filter{}, 3},
		{"按工具筛", Filter{Tool: "db_query"}, 2},
		{"按来源筛", Filter{Source: model.MCPSourceAgent}, 2},
		{"只看失败", Filter{ErrorsOnly: true}, 2},
		{"工具与来源同时收窄", Filter{Tool: "db_query", Source: model.MCPSourceManual}, 1},
		{"失败叠加工具", Filter{Tool: "db_query", ErrorsOnly: true}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, total, err := svc.List(ctx, tt.filter, 1, 20)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if total != tt.want {
				t.Errorf("total = %d, want %d", total, tt.want)
			}
		})
	}
}

// 契约：最新的调用排在最前。工具台默认看的就是「刚才那次」。
func TestListNewestFirst(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	for _, tool := range []string{"first", "second", "third"} {
		svc.Record(ctx, model.MCPCall{Server: "acpp-db", Tool: tool})
	}
	rows, _, err := svc.List(ctx, Filter{}, 1, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if rows[0].Tool != "third" || rows[2].Tool != "first" {
		t.Errorf("顺序错了: %s, %s, %s", rows[0].Tool, rows[1].Tool, rows[2].Tool)
	}
}

// 契约：统计按 server+tool 聚合，失败数与平均耗时都要算对——
// 「这个工具从没被调用过」和「调用了但一半在报错」是两种完全不同的结论。
func TestStatsAggregation(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	seed := []model.MCPCall{
		{Server: "acpp-db", Tool: "db_query", DurationMs: 10},
		{Server: "acpp-db", Tool: "db_query", DurationMs: 30, IsError: true},
		{Server: "acpp-db", Tool: "db_schema", DurationMs: 5},
	}
	for _, rec := range seed {
		svc.Record(ctx, rec)
	}

	stats, err := svc.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("聚合出 %d 组, want 2", len(stats))
	}
	// 次数降序：db_query 两次排前面。
	top := stats[0]
	if top.Tool != "db_query" || top.Count != 2 || top.ErrorCount != 1 || top.AvgMs != 20 {
		t.Errorf("db_query 统计 = %+v", top)
	}
	if stats[1].Tool != "db_schema" || stats[1].ErrorCount != 0 {
		t.Errorf("db_schema 统计 = %+v", stats[1])
	}
	// 最近使用时间要真的解析出来：聚合列没有 schema 信息，扫错了就是零值，
	// 页面上「最近使用」会静默变成 1970。
	if top.LastUsedAt.IsZero() {
		t.Error("最近使用时间没解析出来")
	}
}

// 契约：清空是真的清空——用户按下「清空记录」后，页面上不该还剩几条。
func TestClear(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	svc.Record(ctx, model.MCPCall{Server: "acpp-db", Tool: "db_query"})
	if err := svc.Clear(ctx); err != nil {
		t.Fatalf("clear: %v", err)
	}
	_, total, err := svc.List(ctx, Filter{}, 1, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 0 {
		t.Errorf("清空后还剩 %d 条", total)
	}
}

// 契约：留存上限之外的旧记录会被裁掉，且裁的是**最老**的那批。
// 不裁的话这张表会随会话数无限长；裁错方向就把刚发生的调用删了。
func TestSweepDropsOldest(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	// 写满上限再多写一批，触发 sweep（每 sweepEvery 次写入裁一次）。
	total := retention + sweepEvery
	for i := 0; i < total; i++ {
		svc.Record(ctx, model.MCPCall{Server: "acpp-db", Tool: "db_query", DurationMs: int64(i)})
	}

	rows, count, err := svc.List(ctx, Filter{}, 1, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if count > retention {
		t.Errorf("留存 %d 条，超过上限 %d", count, retention)
	}
	// 最新那条必须还在：裁掉的应该是队尾而不是队头。
	if rows[0].DurationMs != int64(total-1) {
		t.Errorf("最新一条被裁掉了: DurationMs = %d, want %d", rows[0].DurationMs, total-1)
	}
}
