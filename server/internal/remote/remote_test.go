package remote

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

func testService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "srv.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Server{}, &model.DataSource{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewService(gdb, nil, "127.0.0.1:48080"), gdb
}

func ptr[T any](v T) *T { return &v }

// 契约：凭证留空表示「不修改」而不是「清空」。编辑时后端不下发密码，
// 表单那格就是空的，提交会原样带回一个空串——按字面处理的话，改一次
// 备注就把生产机的密码清了。
func TestService_Update_KeepsCredentials(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, Input{
		Name: "box", Host: "10.0.0.1", User: "root",
		Auth: "password", Password: ptr("s3cret"),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !created.HasPassword {
		t.Fatal("新建后应标记已设密码")
	}

	// 只改备注，密码字段留空（前端编辑态的真实形状）。
	updated, err := svc.Update(ctx, created.ID, Input{
		Name: "box", Host: "10.0.0.1", User: "root",
		Auth: "password", Password: ptr(""), Note: "生产机",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Note != "生产机" {
		t.Fatalf("备注应已更新，得到 %q", updated.Note)
	}
	if !updated.HasPassword {
		t.Fatal("留空的密码不得被清掉")
	}

	stored, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Password != "s3cret" {
		t.Fatalf("库里的密码应保持原值，得到 %q", stored.Password)
	}
}

// 契约：被数据源当跳板机用着的服务器不能删——删掉的话那条数据源会无声
// 地拨不出去，错误要等到下次查询才浮现。
func TestService_Delete_BlockedByDataSourceRef(t *testing.T) {
	svc, gdb := testService(t)
	ctx := context.Background()

	srv, err := svc.Create(ctx, Input{Name: "jump", Host: "1.2.3.4", User: "root", Auth: "password", Password: ptr("x")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ds := model.DataSource{
		Project: "p", Env: "pre", Host: "127.0.0.1", Port: 3306, User: "root",
		Database: "d", SSHEnabled: true, ServerID: srv.ID,
	}
	if err := gdb.Create(&ds).Error; err != nil {
		t.Fatalf("create datasource: %v", err)
	}

	err = svc.Delete(ctx, srv.ID)
	if err == nil {
		t.Fatal("被引用的服务器必须拒绝删除")
	}
	if !strings.Contains(err.Error(), "跳板机") {
		t.Fatalf("错误应说明它正被当跳板机用: %v", err)
	}

	// 数据源改成不引用后即可删除。
	if err := gdb.Model(&ds).Update("server_id", 0).Error; err != nil {
		t.Fatalf("unlink: %v", err)
	}
	if err := svc.Delete(ctx, srv.ID); err != nil {
		t.Fatalf("解除引用后应可删除: %v", err)
	}
	if _, err := svc.Get(ctx, srv.ID); err != service.ErrNotFound {
		t.Fatalf("删除后应查不到，得到 %v", err)
	}
}

// 契约：迁移把数据源里的 SSH 配置搬进服务器表，共用同一台跳板的多条数据源
// 只建一条服务器记录，且跑第二遍什么都不做（幂等）。
func TestService_MigrateFromDataSources(t *testing.T) {
	svc, gdb := testService(t)
	ctx := context.Background()

	// 两条数据源共用一台跳板机，第三条用另一台，第四条不开隧道。
	rows := []model.DataSource{
		{Project: "pp", Env: "pre", Host: "127.0.0.1", Port: 3306, User: "root", Database: "a",
			SSHEnabled: true, SSHHost: "10.0.0.9", SSHPort: 2220, SSHUser: "root",
			SSHAuth: "key", SSHKeyPath: "~/.ssh/id_ed25519"},
		{Project: "pp", Env: "prod", Host: "127.0.0.1", Port: 3306, User: "root", Database: "b",
			SSHEnabled: true, SSHHost: "10.0.0.9", SSHPort: 2220, SSHUser: "root",
			SSHAuth: "key", SSHKeyPath: "~/.ssh/id_ed25519"},
		{Project: "other", Env: "dev", Host: "127.0.0.1", Port: 3306, User: "root", Database: "c",
			SSHEnabled: true, SSHHost: "10.0.0.8", SSHPort: 22, SSHUser: "deploy",
			SSHAuth: "password", SSHPassword: "pw"},
		{Project: "local", Env: "local", Host: "127.0.0.1", Port: 3306, User: "root", Database: "d"},
	}
	for i := range rows {
		if err := gdb.Create(&rows[i]).Error; err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	if err := svc.MigrateFromDataSources(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	servers, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("共用跳板的两条应合成一台，期望 2 台服务器，得到 %d: %+v", len(servers), servers)
	}

	byHost := map[string]model.Server{}
	for _, s := range servers {
		byHost[s.Host] = s
	}
	jump := byHost["10.0.0.9"]
	if jump.Port != 2220 || jump.User != "root" || jump.Auth != "key" {
		t.Fatalf("跳板机字段应原样搬过来，得到 %+v", jump)
	}
	if jump.KeyPath != "~/.ssh/id_ed25519" {
		t.Fatalf("私钥路径应保留，得到 %q", jump.KeyPath)
	}
	if !strings.Contains(jump.Note, "迁移") {
		t.Fatalf("备注应说明来历，得到 %q", jump.Note)
	}
	if byHost["10.0.0.8"].Password != "pw" {
		t.Fatal("密码应原样搬过来")
	}

	// 两条 pp 的数据源都指向同一台，不开隧道的那条不受影响。
	var got []model.DataSource
	if err := gdb.Order("env").Find(&got).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	linked := map[string]uint{}
	for _, d := range got {
		linked[d.Project+"/"+d.Env] = d.ServerID
	}
	if linked["pp/pre"] == 0 || linked["pp/pre"] != linked["pp/prod"] {
		t.Fatalf("共用跳板的数据源应指向同一台服务器: %+v", linked)
	}
	if linked["other/dev"] == linked["pp/pre"] {
		t.Fatal("不同跳板机的数据源不该指向同一台")
	}
	if linked["local/local"] != 0 {
		t.Fatal("没开隧道的数据源不该被关联")
	}

	// 幂等：再跑一遍不新增、不改动。
	if err := svc.MigrateFromDataSources(ctx); err != nil {
		t.Fatalf("migrate again: %v", err)
	}
	again, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list again: %v", err)
	}
	if len(again) != 2 {
		t.Fatalf("重复迁移不得新增记录，得到 %d 台", len(again))
	}
}

// 契约：名字是 AI 调工具时填的标识，必须唯一且不含会让它难以复述的字符。
func TestService_Create_NameRules(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	base := Input{Name: "box", Host: "h", User: "u", Auth: "password", Password: ptr("p")}
	if _, err := svc.Create(ctx, base); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Create(ctx, base); err == nil {
		t.Fatal("重名必须被拒绝")
	}

	bad := []Input{
		{Host: "h", User: "u", Auth: "password", Password: ptr("p")},              // 缺名字
		{Name: "a b", Host: "h", User: "u", Auth: "password", Password: ptr("p")}, // 名字带空格
		{Name: "a/b", Host: "h", User: "u", Auth: "password", Password: ptr("p")}, // 名字带斜杠
		{Name: "c", User: "u", Auth: "password", Password: ptr("p")},              // 缺主机
		{Name: "c", Host: "h", Auth: "password", Password: ptr("p")},              // 缺用户
		{Name: "c", Host: "h", User: "u", Auth: "magic", Password: ptr("p")},      // 未知验证方式
	}
	for i, in := range bad {
		if _, err := svc.Create(ctx, in); err == nil {
			t.Errorf("第 %d 条非法入参应被拒绝: %+v", i, in)
		}
	}
}

// 契约：ByName 是 AI 那一侧的入口——查不到要给 ErrNotFound，
// 好让上层把「没有叫 x 的服务器」这句话原样说给模型听。
func TestService_ByName(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, Input{
		Name: "pp-game-live", Host: "1.1.1.1", User: "root",
		Auth: "key", KeyPath: "/k", Note: "生产机",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := svc.ByName(ctx, "pp-game-live")
	if err != nil {
		t.Fatalf("by name: %v", err)
	}
	if got.Note != "生产机" {
		t.Fatalf("备注应带出来（它会给 AI 看），得到 %q", got.Note)
	}
	if _, err := svc.ByName(ctx, "nope"); err != service.ErrNotFound {
		t.Fatalf("不存在的名字应返回 ErrNotFound，得到 %v", err)
	}
}

// 契约：列表页那个「测试连接」按钮不带表单内容，空入参必须表示「就测这条」
// 而不是「把它清空再测」。真机验证时踩到过：空 body 直接报「名称不能为空」。
func TestService_Test_EmptyInputKeepsRecord(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, Input{
		Name: "box", Host: "127.0.0.1", Port: 1, User: "root",
		Auth: "password", Password: ptr("pw"),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// 端口 1 上没有 ssh，连接必然失败——要的是**失败的原因**：
	// 必须是连不上，而不是校验把记录判成了空。
	_, err = svc.Test(ctx, created.ID, Input{})
	if err == nil {
		t.Fatal("连不通的地址应当报错")
	}
	if strings.Contains(err.Error(), "不能为空") {
		t.Fatalf("空入参不该把已存记录清空: %v", err)
	}

	// 新建路径（id=0）相反：空入参就该被校验挡住，并说清缺什么。
	if _, err := svc.Test(ctx, 0, Input{}); err == nil || !strings.Contains(err.Error(), "不能为空") {
		t.Fatalf("新建时的空入参应被校验挡住，得到 %v", err)
	}
}
