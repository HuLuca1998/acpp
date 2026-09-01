package datasource

import (
	"context"
	"strings"
	"testing"

	"acpp/server/internal/model"
)

// fakeServers 是 Servers 接口的测试替身：只认 id，查不到就报错，
// 用来验证「服务器没了」这条降级路径。
type fakeServers struct {
	byID  map[uint]*model.Server
	calls int
}

func (f *fakeServers) ServerByID(_ context.Context, id uint) (*model.Server, error) {
	f.calls++
	if srv, ok := f.byID[id]; ok {
		return srv, nil
	}
	return nil, errNoServer
}

var errNoServer = &notFoundErr{}

type notFoundErr struct{}

func (*notFoundErr) Error() string { return "not found" }

// 契约：读出来的数据源要带上跳板机配置。这是 adr-019 之后拨号的前提——
// 漏填的路径上，开着隧道的数据源会在 connect 里报「没有关联跳板机」。
func TestService_Finish_AttachesServer(t *testing.T) {
	svc := testService(t, t.TempDir())
	ctx := context.Background()
	fake := &fakeServers{byID: map[uint]*model.Server{
		7: {ID: 7, Name: "pp-game-live", Host: "10.0.0.9", Port: 2220, User: "root"},
	}}
	svc.WithServers(fake)

	created, err := svc.Create(ctx, Input{
		Project: "pp", Env: "pre", Host: "127.0.0.1", User: "root", Database: "d",
		SSHEnabled: ptr(true), ServerID: ptr(uint(7)),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Server == nil || created.Server.Name != "pp-game-live" {
		t.Fatalf("新建返回值就该带上跳板机，得到 %+v", created.Server)
	}
	if created.ServerName != "pp-game-live" {
		t.Fatalf("展示用的 ServerName 应填好，得到 %q", created.ServerName)
	}

	// 三条读取路径都要填充：Get / List / ForCwd 用的 ForScope。
	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Server == nil {
		t.Fatal("Get 必须填充跳板机")
	}
	list, _, err := svc.List(ctx, 1, 10, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Server == nil {
		t.Fatal("List 必须填充跳板机")
	}
	scoped, err := svc.ForScope(ctx, Scope{Only: created.ID}, true)
	if err != nil {
		t.Fatalf("for scope: %v", err)
	}
	if len(scoped) != 1 || scoped[0].Server == nil {
		t.Fatal("锁定单条的路径也必须填充跳板机")
	}
}

// 契约：跳板机被删掉时，数据源本身仍读得出来（列表页不该整个 500），
// 只是 Server 留空——真正的报错留到拨号那一刻，那里能说清是哪条连接。
func TestService_Finish_MissingServerDegrades(t *testing.T) {
	svc := testService(t, t.TempDir())
	ctx := context.Background()
	svc.WithServers(&fakeServers{byID: map[uint]*model.Server{}})

	created, err := svc.Create(ctx, Input{
		Project: "pp", Env: "pre", Host: "127.0.0.1", User: "root", Database: "d",
		SSHEnabled: ptr(true), ServerID: ptr(uint(99)),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("跳板机没了也要能读出数据源: %v", err)
	}
	if got.Server != nil {
		t.Fatal("查不到的跳板机应留空")
	}

	// 而拨号必须当场失败，且说清怎么修。
	_, err = connect(ctx, got, "")
	if err == nil {
		t.Fatal("没有跳板机时拨号必须失败")
	}
	if !strings.Contains(err.Error(), "跳板机") {
		t.Fatalf("报错要指明缺跳板机: %v", err)
	}
}

// 契约：不开隧道的数据源不查服务器表，也不该因为没配服务器面而出问题。
func TestService_Finish_SkipsWhenNoTunnel(t *testing.T) {
	svc := testService(t, t.TempDir())
	ctx := context.Background()
	fake := &fakeServers{byID: map[uint]*model.Server{}}
	svc.WithServers(fake)

	if _, err := svc.Create(ctx, Input{
		Project: "pp", Env: "local", Host: "127.0.0.1", User: "root", Database: "d",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if fake.calls != 0 {
		t.Fatalf("没开隧道不该去查服务器，实际查了 %d 次", fake.calls)
	}
}

// 契约：导出的 URI 里 SSH 段来自关联的服务器。这条链接是给人贴进 Navicat 的，
// 跳板机信息错了会连到别的机器上去。
func TestExportURI_SSHFromServer(t *testing.T) {
	src := &model.DataSource{
		Project: "pp", Env: "pre", Host: "127.0.0.1", Port: 3306,
		User: "root", Password: "dbpw", Database: "pp_game",
		SSHEnabled: true, ServerID: 7,
		Server: &model.Server{
			Name: "pp-game-live", Host: "10.0.0.9", Port: 2220,
			User: "deploy", Auth: SSHAuthKey, KeyPath: "/keys/id",
		},
	}
	out := ExportURI(src)
	for _, want := range []string{"10.0.0.9", "2220", "deploy", "PublicKey", "/keys/id"} {
		if !strings.Contains(out.Navicat, want) {
			t.Errorf("Navicat URI 少了跳板机信息 %q: %s", want, out.Navicat)
		}
	}
	if !strings.Contains(out.Standard, "sshHost=10.0.0.9") {
		t.Errorf("标准 URI 应带跳板机: %s", out.Standard)
	}

	// 关联丢失时整段不写——导一条指向不存在跳板机的链接更容易误导人。
	src.Server = nil
	out = ExportURI(src)
	if strings.Contains(out.Navicat, "Conn.SSH") || strings.Contains(out.Standard, "sshHost") {
		t.Errorf("没有跳板机时不该写 SSH 段: %s | %s", out.Navicat, out.Standard)
	}
}
