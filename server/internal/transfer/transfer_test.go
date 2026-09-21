package transfer

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/datasource"
	"acpp/server/internal/model"
	"acpp/server/internal/remote"
)

// 真实形态的连接配置：一台跳板机 + 一条走它的线上库 + 一条直连的本地库。
var (
	jumpHost = remote.Input{
		Name: "shop-live", Host: "203.0.113.24", Port: 22, User: "deploy",
		Auth: "key", KeyPath: "~/.ssh/id_ed25519", Note: "shop 生产机，项目在 /srv/shop",
	}
	prodDB = datasource.Input{
		Project: "shop", Env: "prod", Host: "127.0.0.1", Port: 3306,
		User: "reader", Database: "shop_main", Params: "charset=utf8mb4",
		Note: "线上主库，只读账号",
	}
	localDB = datasource.Input{
		Project: "shop", Env: "local", Host: "127.0.0.1", Port: 3306,
		User: "root", Database: "shop_dev",
	}
)

func newStack(t *testing.T) (*Service, *remote.Service, *datasource.Service) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cfg.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Server{}, &model.DataSource{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	servers := remote.NewService(gdb, nil, "127.0.0.1:48080")
	sources := datasource.NewService(gdb, nil, "127.0.0.1:48080")
	return New(servers, sources), servers, sources
}

func ptr[T any](v T) *T { return &v }

// seed 建一台跳板机与两条数据源（一条走跳板、一条直连）。
func seed(t *testing.T, servers *remote.Service, sources *datasource.Service) {
	t.Helper()
	ctx := context.Background()
	pw := "hunter2"
	in := jumpHost
	in.Password = &pw
	srv, err := servers.Create(ctx, in)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	prod := prodDB
	prod.SSHEnabled = ptr(true)
	prod.ServerID = ptr(srv.ID)
	dbPw := "s3cret"
	prod.Password = &dbPw
	if _, err := sources.Create(ctx, prod); err != nil {
		t.Fatalf("create prod source: %v", err)
	}
	if _, err := sources.Create(ctx, localDB); err != nil {
		t.Fatalf("create local source: %v", err)
	}
}

func exportLines(t *testing.T, svc *Service) []string {
	t.Helper()
	var buf bytes.Buffer
	if err := svc.Export(context.Background(), &buf); err != nil {
		t.Fatalf("export: %v", err)
	}
	return strings.Split(strings.TrimSpace(buf.String()), "\n")
}

// 契约：导出的 jsonl 一行一条记录，服务器排在数据源前面——导入是顺序读的，
// 数据源引用的跳板机必须先建好。
func TestService_Export_ServersComeBeforeSources(t *testing.T) {
	svc, servers, sources := newStack(t)
	seed(t, servers, sources)

	lines := exportLines(t, svc)

	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	var kinds []string
	for _, l := range lines {
		var head struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(l), &head); err != nil {
			t.Fatalf("line is not json: %s", l)
		}
		kinds = append(kinds, head.Kind)
	}
	if kinds[0] != KindServer {
		t.Fatalf("kinds = %v, want the server first", kinds)
	}
	if kinds[1] != KindSource || kinds[2] != KindSource {
		t.Fatalf("kinds = %v, want both data sources after it", kinds)
	}
}

// 契约：导出**不含凭证**。密码与私钥口令在这个项目里永不出 API，搬家文件
// 会被人随手丢进网盘或聊天窗口，更不能带。
func TestService_Export_CarriesNoSecrets(t *testing.T) {
	svc, servers, sources := newStack(t)
	seed(t, servers, sources)

	body := strings.Join(exportLines(t, svc), "\n")

	for _, secret := range []string{"hunter2", "s3cret", "password", "passphrase"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(secret)) {
			t.Fatalf("export leaked %q:\n%s", secret, body)
		}
	}
	// 但连得到哪儿、用哪个账号要留着，否则搬过去等于没搬。
	if !strings.Contains(body, "203.0.113.24") || !strings.Contains(body, "reader") {
		t.Fatalf("export lost the connection facts:\n%s", body)
	}
}

// 契约：跳板机按**名字**引用。id 是本机自增的，换台机器必然对不上——导入
// 时靠名字把数据源接回它的跳板机。
func TestService_ImportZipRoundTrip_RelinksJumpHostByName(t *testing.T) {
	src, servers, sources := newStack(t)
	seed(t, servers, sources)
	var buf bytes.Buffer
	if err := src.Export(context.Background(), &buf); err != nil {
		t.Fatalf("export: %v", err)
	}

	dst, dstServers, dstSources := newStack(t)
	// 新机器上先塞一条无关服务器，让 id 错开——这正是 id 不能直接搬的原因。
	if _, err := dstServers.Create(context.Background(), remote.Input{
		Name: "unrelated", Host: "198.51.100.7", Port: 22, User: "ops", Auth: "password",
	}); err != nil {
		t.Fatalf("create decoy: %v", err)
	}

	res, err := dst.Import(context.Background(), bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	if len(res.Imported) != 3 {
		t.Fatalf("imported = %v, want 3 records", res.Imported)
	}
	list, _, err := dstSources.List(context.Background(), datasource.ListFilter{Keyword: "shop_main"}, 1, 20, "")
	if err != nil || len(list) != 1 {
		t.Fatalf("find imported source: err=%v list=%+v", err, list)
	}
	got := list[0]
	if !got.SSHEnabled {
		t.Fatal("imported source lost its tunnel flag")
	}
	jump, err := dstServers.Get(context.Background(), got.ServerID)
	if err != nil {
		t.Fatalf("resolve jump host: %v", err)
	}
	if jump.Name != "shop-live" {
		t.Fatalf("tunnel points at %q, want shop-live", jump.Name)
	}
	if got.Host != "127.0.0.1" || got.User != "reader" || got.Database != "shop_main" {
		t.Fatalf("connection facts changed: %+v", got)
	}
}

// 契约：导入回来的条目都要补凭证——导出不带密码，不说清楚的话人会以为
// 搬完就能连。
func TestService_Import_ReportsWhatStillNeedsSecrets(t *testing.T) {
	src, servers, sources := newStack(t)
	seed(t, servers, sources)
	var buf bytes.Buffer
	if err := src.Export(context.Background(), &buf); err != nil {
		t.Fatalf("export: %v", err)
	}

	dst, _, _ := newStack(t)
	res, err := dst.Import(context.Background(), bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	joined := strings.Join(res.NeedSecret, " ")
	for _, want := range []string{"server:shop-live", "datasource:shop/prod", "datasource:shop/local"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("needSecret = %v, missing %s", res.NeedSecret, want)
		}
	}
}

// 契约：同名的跳过而不是覆盖——那是这台机器上用户自己配的连接。
func TestService_Import_SkipsExisting(t *testing.T) {
	src, servers, sources := newStack(t)
	seed(t, servers, sources)
	var buf bytes.Buffer
	if err := src.Export(context.Background(), &buf); err != nil {
		t.Fatalf("export: %v", err)
	}

	dst, dstServers, dstSources := newStack(t)
	seed(t, dstServers, dstSources)

	res, err := dst.Import(context.Background(), bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	if len(res.Imported) != 0 {
		t.Fatalf("imported = %v, want nothing", res.Imported)
	}
	if len(res.Skipped) != 3 {
		t.Fatalf("skipped = %+v, want all three", res.Skipped)
	}
	for _, s := range res.Skipped {
		if s.Reason != "exists" {
			t.Fatalf("skip reason = %+v, want exists", s)
		}
	}
}

// 契约：引用了不存在跳板机的数据源被跳过并说明原因。硬建出来只会得到一条
// 连不通的连接，而人不会知道少了什么。
func TestService_Import_SkipsSourceWithMissingJumpHost(t *testing.T) {
	dst, _, _ := newStack(t)
	line, err := json.Marshal(SourceRecord{
		Kind: KindSource, Project: "shop", Env: "prod", Host: "127.0.0.1", Port: 3306,
		User: "reader", Database: "shop_main", SSHEnabled: true, Server: "gone",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	res, err := dst.Import(context.Background(), bytes.NewReader(line))
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	if len(res.Imported) != 0 {
		t.Fatalf("imported = %v, want nothing", res.Imported)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Reason != "unknown_server" {
		t.Fatalf("skipped = %+v, want one unknown_server", res.Skipped)
	}
}

// 契约：坏行只跳过自己。一行一条正是选 jsonl 的理由——半份文件读不进去
// 比读进去一半还糟。
func TestService_Import_BadLineDoesNotSinkTheFile(t *testing.T) {
	dst, _, _ := newStack(t)
	good, err := json.Marshal(ServerRecord{
		Kind: KindServer, Name: "shop-live", Host: "203.0.113.24", Port: 22,
		User: "deploy", Auth: "password",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := "{ 这行不是 json\n" + string(good) + "\n\n"

	res, err := dst.Import(context.Background(), strings.NewReader(body))
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	if len(res.Imported) != 1 || res.Imported[0] != "server:shop-live" {
		t.Fatalf("imported = %v, want the good line to land", res.Imported)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Reason != "invalid" {
		t.Fatalf("skipped = %+v, want one invalid line", res.Skipped)
	}
}
