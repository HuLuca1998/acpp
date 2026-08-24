package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/model"
)

// 契约：刚建出来的会话是 idle，不是 active。
//
// active 的语义是「有一轮正在跑」，只由 chat_turn 在发起一轮时置上、轮末
// 归回。新建时若写成 active，前端 bootstrap 会据此把 busy 置 true，而一条
// 从没跑过的会话不会有任何轮末事件来复位它——发送按钮永远停在「中止」，
// 这条会话再也发不出消息，只能刷新页面。
//
// 这个 bug 平时踩不到：从界面新建后一般紧接着就发第一句，那一轮把状态带
// 正了。只要「建完不发就切走」或用 API 建会话就必中，所以要钉死。
func TestCreateSessionStartsIdle(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "state.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Agent{}, &model.Session{}, &model.Tenant{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	agent := model.Agent{Name: "claude", Command: "claude"}
	if err := gdb.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	root := t.TempDir()
	svc := NewSessionService(gdb)
	view, err := svc.Create(context.Background(), TenantScope(1, root),
		SessionInput{AgentID: agent.ID, Title: "刚建的会话"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if view.State != model.SessionIdle {
		t.Errorf("新建会话 state = %q，期望 %q——一轮都没跑过的会话不该是 active，"+
			"前端会据此把界面卡在 busy，发送按钮变成「中止」再也发不出消息",
			view.State, model.SessionIdle)
	}

	// 落库的那份也要对：前端 bootstrap 读的是查询结果，不是创建时的返回值。
	var saved model.Session
	if err := gdb.First(&saved, view.ID).Error; err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if saved.State != model.SessionIdle {
		t.Errorf("落库的 state = %q，期望 %q", saved.State, model.SessionIdle)
	}
}
