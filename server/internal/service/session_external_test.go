package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/model"
)

// externalFixture 建一套带两个内置工具的库。
func externalFixture(t *testing.T) *SessionService {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ext.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Agent{}, &model.Session{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, name := range []string{"claude", "codex"} {
		if err := gdb.Create(&model.Agent{Name: name, Command: name + "-acp", Flavor: name}).Error; err != nil {
			t.Fatalf("seed agent %s: %v", name, err)
		}
	}
	return NewSessionService(gdb)
}

// 登记必须幂等：子区的状态文件丢了、同一个子区被重复触发，都只能拿回
// 同一条记录——否则每聊一次就多一条会话，账也跟着分家。
func TestEnsureExternalIsIdempotent(t *testing.T) {
	svc := externalFixture(t)
	ctx := context.Background()
	in := ExternalSession{
		Key:       "dc:1418209934096961607",
		AgentName: "claude",
		Title:     "订单对账脚本",
		Cwd:       t.TempDir(),
		Origin:    model.SessionOriginDiscord,
	}

	first, err := svc.EnsureExternal(ctx, in)
	if err != nil {
		t.Fatalf("EnsureExternal: %v", err)
	}
	// 第二次连标题都变了（子区被改过名），仍然是同一条。
	in.Title = "改了个名"
	second, err := svc.EnsureExternal(ctx, in)
	if err != nil {
		t.Fatalf("EnsureExternal 第二次: %v", err)
	}
	if first != second {
		t.Errorf("两次登记拿到 %d / %d，期望同一条", first, second)
	}

	var count int64
	if err := svc.db.Model(&model.Session{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("会话表里有 %d 条，期望 1 条", count)
	}
}

func TestEnsureExternalRecordsOwnership(t *testing.T) {
	svc := externalFixture(t)
	cwd := t.TempDir()
	id, err := svc.EnsureExternal(context.Background(), ExternalSession{
		Key:       "dc:42",
		AgentName: "codex",
		Title:     "巡检",
		Cwd:       cwd,
		Origin:    model.SessionOriginCron,
	})
	if err != nil {
		t.Fatalf("EnsureExternal: %v", err)
	}

	var sess model.Session
	if err := svc.db.First(&sess, id).Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	if sess.Origin != model.SessionOriginCron {
		t.Errorf("Origin = %q，定时任务开的该记 cron", sess.Origin)
	}
	if sess.TenantID != 0 {
		t.Errorf("TenantID = %d，bot 是 owner 开的，账记在他名下", sess.TenantID)
	}
	if sess.Cwd != cwd || sess.ExternalKey != "dc:42" {
		t.Errorf("归属没落全: cwd=%q key=%q", sess.Cwd, sess.ExternalKey)
	}
	if sess.State != model.SessionIdle {
		t.Errorf("State = %q，刚登记的会话一轮都没跑过", sess.State)
	}
}

// 认不出的工具名要当场报错，而不是落一条 agentId=0 的孤儿会话。
func TestEnsureExternalRejectsUnknownAgent(t *testing.T) {
	svc := externalFixture(t)
	_, err := svc.EnsureExternal(context.Background(), ExternalSession{
		Key: "dc:1", AgentName: "gemini", Origin: model.SessionOriginDiscord,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v，期望 ErrNotFound", err)
	}
	_, err = svc.EnsureExternal(context.Background(), ExternalSession{AgentName: "claude"})
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("没有 key 时 err = %v，期望 ErrInvalid", err)
	}
}

// 来源筛选要认得新的两种，否则 Discord 的会话在列表里筛不出来。
func TestListFiltersByNewOrigins(t *testing.T) {
	svc := externalFixture(t)
	ctx := context.Background()
	for _, in := range []ExternalSession{
		{Key: "dc:1", AgentName: "claude", Origin: model.SessionOriginDiscord},
		{Key: "dc:2", AgentName: "claude", Origin: model.SessionOriginDiscord},
		{Key: "dc:3", AgentName: "codex", Origin: model.SessionOriginCron},
	} {
		if _, err := svc.EnsureExternal(ctx, in); err != nil {
			t.Fatalf("EnsureExternal %s: %v", in.Key, err)
		}
	}
	// 一条界面里开的，验证 user 过滤不会把子区会话捎上。
	if err := svc.db.Create(&model.Session{AgentID: 1, State: model.SessionIdle}).Error; err != nil {
		t.Fatalf("seed ui session: %v", err)
	}

	for _, tc := range []struct {
		origin string
		want   int64
	}{
		{model.SessionOriginDiscord, 2},
		{model.SessionOriginCron, 1},
		{SessionOriginUser, 1},
		{SessionOriginAny, 4},
	} {
		_, total, err := svc.List(ctx, OwnerScope(), SessionFilter{Origin: tc.origin}, 1, 20, "")
		if err != nil {
			t.Fatalf("List(origin=%q): %v", tc.origin, err)
		}
		if total != tc.want {
			t.Errorf("List(origin=%q) = %d 条，期望 %d", tc.origin, total, tc.want)
		}
	}

	if _, _, err := svc.List(ctx, OwnerScope(), SessionFilter{Origin: "nope"}, 1, 20, ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("认不出的来源 err = %v，期望 ErrInvalid", err)
	}
}
