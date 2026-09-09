package service

import (
	"context"
	"testing"

	"acpp/server/internal/model"
)

// 契约：关键词按标题子串过滤且通配符不生效；状态精确匹配；两者可叠加。
func TestSessionService_ListFilter(t *testing.T) {
	svc, alice, _, idA, _ := scopedSessions(t)
	ctx := context.Background()
	if _, err := svc.Create(ctx, alice, SessionInput{AgentID: 1, Title: "deploy_prod"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.db.Model(&model.Session{}).Where("id = ?", idA).Update("state", model.SessionEnded).Error; err != nil {
		t.Fatalf("update state: %v", err)
	}

	list, total, err := svc.List(ctx, alice, SessionFilter{Keyword: "y_p"}, 1, 50, "")
	if err != nil {
		t.Fatalf("List(keyword): %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].Title != "deploy_prod" {
		t.Fatalf("keyword y_p: got total=%d list=%+v", total, list)
	}
	if _, total, _ = svc.List(ctx, alice, SessionFilter{Keyword: "yxp"}, 1, 50, ""); total != 0 {
		t.Fatalf("underscore must not act as wildcard, got total=%d", total)
	}
	if _, total, _ = svc.List(ctx, alice, SessionFilter{State: string(model.SessionEnded)}, 1, 50, ""); total != 1 {
		t.Fatalf("state=ended: got total=%d", total)
	}
	if _, total, _ = svc.List(ctx, alice, SessionFilter{State: string(model.SessionEnded), Keyword: "deploy"}, 1, 50, ""); total != 0 {
		t.Fatalf("state+keyword must intersect, got total=%d", total)
	}
}
