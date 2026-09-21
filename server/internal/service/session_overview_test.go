package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/model"
)

// 契约：概览的列表字段序列化成 JSON 后永远是数组，**一条会话都没有时也是
// `[]` 而不是 `null`**。
//
// 这是全新安装的首屏：前端按契约（types/acp.ts 声明的是数组）直接
// `byState.find(...)`，拿到 null 就整棵 React 树崩掉——用户看到的是一片
// 纯黑的窗口，什么都点不了，也没有任何报错提示。
func TestSessionService_Overview_EmptyLibraryYieldsArraysNotNull(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "fresh.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Agent{}, &model.Session{}, &model.Tenant{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	svc := NewSessionService(gdb)

	stats, err := svc.Overview(context.Background(), OwnerScope(), 14)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}

	raw, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	for _, field := range []string{`"byAgent":[]`, `"byState":[]`} {
		if !strings.Contains(body, field) {
			t.Fatalf("fresh install must serialize %s; got %s", field, body)
		}
	}
}
