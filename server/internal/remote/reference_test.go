package remote

import (
	"context"
	"strings"
	"testing"

	"acpp/server/internal/service"
)

// 契约：引用展开成 resource 块的内容——URI 认得出出处，正文说清「去看它」。
func TestService_Reference(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, Input{
		Name: "pp-game-live", Host: "10.0.0.9", Port: 2220, User: "root",
		Auth: "key", KeyPath: "/k", Note: "pp-game 生产机，项目在 /srv/pp-game-live",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	refs, err := svc.Reference(ctx, []string{"pp-game-live"})
	if err != nil {
		t.Fatalf("reference: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("期望 1 条引用，得到 %d", len(refs))
	}

	got := refs[0]
	if got.URI != "acpp-server://pp-game-live" {
		t.Errorf("URI 形状不对: %q", got.URI)
	}
	// 转录重建按这个前缀把服务器芯片与文件芯片分开，两侧必须一致。
	if !strings.HasPrefix(got.URI, service.ServerRefScheme) {
		t.Errorf("URI 前缀要与 service.ServerRefScheme 一致: %q", got.URI)
	}

	for _, want := range []string{"pp-game-live", "root@10.0.0.9:2220", "pp-game 生产机"} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("告知里少了 %q:\n%s", want, got.Text)
		}
	}
	// 语气要点：说的是「去用工具看」，不是「这是一份快照」。
	for _, want := range []string{"acpp-server", "server", "只读"} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("告知里少了 %q（模型据此知道该怎么办）:\n%s", want, got.Text)
		}
	}
	// **主线决策**：引用只到机器这一级，远程路径要 AI 自己从代码推断。
	// 告知里必须点明这件事，否则它会把备注里的路径当成唯一事实。
	if !strings.Contains(got.Text, "项目代码") {
		t.Errorf("告知要说明路径从项目代码推断:\n%s", got.Text)
	}
}

// 契约：引用一台不存在的机器要给 ErrNotFound，好让上层把「没有叫 x 的
// 服务器」原样说给用户听，而不是变成一句 500。
func TestService_Reference_NotFound(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	_, err := svc.Reference(context.Background(), []string{"nope"})
	if err == nil {
		t.Fatal("引用不存在的机器必须报错")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("错误里要带上那个名字: %v", err)
	}

	// 停用的机器同样引用不到——工具面里没有它，引用了也没意义。
	off, _ := svc.Create(ctx, Input{Name: "off", Host: "h", User: "u", Auth: "key"})
	if _, err := svc.Update(ctx, off.ID, Input{
		Name: "off", Host: "h", User: "u", Auth: "key", Disabled: ptr(true),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reference(ctx, []string{"off"}); err == nil {
		t.Error("停用的机器不该能被引用")
	}
}

// 契约：一次引用多台时逐条展开，顺序与入参一致。
func TestService_Reference_Multiple(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	for _, n := range []string{"a", "b"} {
		if _, err := svc.Create(ctx, Input{Name: n, Host: "h", User: "u", Auth: "key"}); err != nil {
			t.Fatal(err)
		}
	}

	refs, err := svc.Reference(ctx, []string{"b", "a"})
	if err != nil {
		t.Fatalf("reference: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("期望 2 条，得到 %d", len(refs))
	}
	if !strings.HasSuffix(refs[0].URI, "b") || !strings.HasSuffix(refs[1].URI, "a") {
		t.Errorf("顺序应与入参一致: %q %q", refs[0].URI, refs[1].URI)
	}
}
