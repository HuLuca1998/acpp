package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/acp"
	"acpp/server/internal/config"
	"acpp/server/internal/model"
	"acpp/server/internal/project"
	"acpp/server/internal/service"
	"acpp/server/internal/transcript"
)

// flowEnv 是一套真实装配的服务 + 两个租户，用来端到端验隔离。
//
// 租户视角在界面上测不出来——本机访问永远被判成 owner（loopback），
// 所以隔离的回归保护只能落在这里。
type flowEnv struct {
	handler http.Handler
	alice   *http.Cookie
	bob     *http.Cookie
	base    string
}

func newFlowEnv(t *testing.T) *flowEnv {
	t.Helper()

	dir := t.TempDir()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "flow.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Agent{}, &model.Session{}, &model.Tenant{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := gdb.Create(&model.Agent{Name: "claude", Command: "claude-agent-acp"}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	transcripts, err := transcript.NewStore(filepath.Join(dir, "transcripts"))
	if err != nil {
		t.Fatalf("transcript store: %v", err)
	}
	t.Cleanup(transcripts.CloseAll)

	manager := acp.NewManager(2, 0, filepath.Join(dir, "skillpack"))
	t.Cleanup(manager.CloseAll)

	base := filepath.Join(dir, "workspaces")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}

	sessions := service.NewSessionService(gdb)
	skillUsage := service.NewSkillUsageService(gdb, dir)
	tenants := service.NewTenantService(gdb, base)
	env := &flowEnv{base: base}
	env.handler = NewRouter(config.Config{}, Services{
		Sessions: sessions,
		Chat:     service.NewChatService(gdb, sessions, manager, transcripts, skillUsage),
		Tenants:  tenants,
		Projects: project.NewService(gdb),
	})

	for _, name := range []string{"alice", "bob"} {
		created, err := tenants.Create(t.Context(), service.TenantInput{Name: name})
		if err != nil {
			t.Fatalf("create tenant %s: %v", name, err)
		}
		cookie := &http.Cookie{Name: tenantCookie, Value: created.InviteToken}
		if name == "alice" {
			env.alice = cookie
		} else {
			env.bob = cookie
		}
	}
	return env
}

// as 发一个带身份的请求：cookie 为 nil 表示 owner（从回环地址来）。
func (e *flowEnv) as(t *testing.T, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if cookie != nil {
		req.AddCookie(cookie)
	} else {
		req.RemoteAddr = "127.0.0.1:5000"
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func decodeData[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var body struct {
		Data T `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode %d: %v", rec.Code, err)
	}
	return body.Data
}

// 契约：租户不带 cwd 建的会话落在自己的工作区根里，别人的会话看不见也
// 取不到（按不存在处理），owner 看得见全部。
func TestTenantFlow_SessionIsolation(t *testing.T) {
	env := newFlowEnv(t)

	rec := env.as(t, env.alice, http.MethodPost, "/api/sessions", `{"agentId":1,"title":"alice work"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("alice create session = %d: %s", rec.Code, rec.Body.String())
	}
	aliceSession := decodeData[service.SessionView](t, rec)
	if !strings.Contains(aliceSession.Cwd, "alice") {
		t.Fatalf("alice session cwd = %q, want inside her own root", aliceSession.Cwd)
	}

	rec = env.as(t, env.bob, http.MethodPost, "/api/sessions", `{"agentId":1,"title":"bob work"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bob create session = %d: %s", rec.Code, rec.Body.String())
	}
	bobSession := decodeData[service.SessionView](t, rec)

	// 列表只有自己的。
	rec = env.as(t, env.alice, http.MethodGet, "/api/sessions", "")
	list := decodeData[page[service.SessionView]](t, rec)
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != aliceSession.ID {
		t.Fatalf("alice sees %+v, want only her own session", list.Items)
	}

	// 按 id 直取别人的会话：404 而不是 403（403 会泄露它存在）。
	for _, path := range []string{
		"/api/sessions/%d",
		"/api/sessions/%d/messages",
		"/api/sessions/%d/fs/entries",
		"/api/sessions/%d/git/branches",
	} {
		target := strings.Replace(path, "%d", itoa(bobSession.ID), 1)
		rec = env.as(t, env.alice, http.MethodGet, target, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("alice GET %s = %d, want 404", target, rec.Code)
		}
	}

	// 删也删不掉。
	rec = env.as(t, env.alice, http.MethodDelete, "/api/sessions/"+itoa(bobSession.ID), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("alice delete bob's session = %d, want 404", rec.Code)
	}

	// owner 看得见两条。
	rec = env.as(t, nil, http.MethodGet, "/api/sessions", "")
	ownerList := decodeData[page[service.SessionView]](t, rec)
	if ownerList.Total != 2 {
		t.Fatalf("owner sees %d sessions, want 2", ownerList.Total)
	}
}

// 契约：租户指定的 cwd 必须落在自己 root 内——一条建会话请求就能把 agent
// 开进别人的工作区，是这套隔离最直接的破法。
func TestTenantFlow_SessionCwdGuarded(t *testing.T) {
	env := newFlowEnv(t)
	bobRoot := filepath.Join(env.base, "bob")

	for _, cwd := range []string{bobRoot, env.base, "/etc", "/"} {
		body := `{"agentId":1,"cwd":"` + cwd + `"}`
		rec := env.as(t, env.alice, http.MethodPost, "/api/sessions", body)
		if rec.Code == http.StatusCreated {
			t.Errorf("alice created a session at %q (want rejection)", cwd)
		}
	}

	// 自己 root 下的新子目录是允许的。
	own := filepath.Join(env.base, "alice", "new-project")
	rec := env.as(t, env.alice, http.MethodPost, "/api/sessions",
		`{"agentId":1,"cwd":"`+own+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("alice create in own subdir = %d: %s", rec.Code, rec.Body.String())
	}
}

// 契约：目录浏览从自己的 root 起步、站在 root 上时没有「上一层」、
// root 之外的路径一律拒绝；新建目录同样钉在 root 内。
func TestTenantFlow_DirBrowsingGuarded(t *testing.T) {
	env := newFlowEnv(t)

	rec := env.as(t, env.alice, http.MethodGet, "/api/fs/dirs", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("alice list dirs = %d: %s", rec.Code, rec.Body.String())
	}
	listing := decodeData[service.DirListing](t, rec)
	if !strings.HasSuffix(listing.Path, "alice") {
		t.Fatalf("alice starts at %q, want her own root", listing.Path)
	}
	if listing.Parent != "" {
		t.Fatalf("alice sees parent %q, want none at her root", listing.Parent)
	}

	for _, path := range []string{"/etc", env.base, filepath.Join(env.base, "bob")} {
		rec = env.as(t, env.alice, http.MethodGet, "/api/fs/dirs?path="+path, "")
		if rec.Code == http.StatusOK {
			t.Errorf("alice listed %q (want rejection)", path)
		}
	}

	rec = env.as(t, env.alice, http.MethodPost, "/api/fs/dirs",
		`{"path":"`+filepath.Join(env.base, "bob")+`","name":"sneaky"}`)
	if rec.Code == http.StatusCreated {
		t.Fatal("alice created a directory inside bob's root")
	}
}

// 契约：项目面同样按身份分家——alice 的项目扫的是 alice 的 root。
func TestTenantFlow_ProjectsScoped(t *testing.T) {
	env := newFlowEnv(t)

	// 在 alice 的 root 下造一个「仓库」。
	repo := filepath.Join(env.base, "alice", "org", "repo", ".git")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	rec := env.as(t, env.alice, http.MethodGet, "/api/projects", "")
	alice := decodeData[page[project.Project]](t, rec)
	if len(alice.Items) != 1 || alice.Items[0].Name != "org/repo" {
		t.Fatalf("alice projects = %+v, want org/repo", alice.Items)
	}

	rec = env.as(t, env.bob, http.MethodGet, "/api/projects", "")
	bob := decodeData[page[project.Project]](t, rec)
	if len(bob.Items) != 0 {
		t.Fatalf("bob sees alice's projects: %+v", bob.Items)
	}

	// 克隆目标也钉在自己 root 内：名字里的上跳要被挡住。
	rec = env.as(t, env.alice, http.MethodPost, "/api/projects/clone",
		`{"url":"https://github.com/org/repo.git","name":"../bob/stolen"}`)
	if rec.Code == http.StatusAccepted {
		t.Fatal("alice started a clone into bob's root")
	}
}

func itoa(v uint) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

var _ = time.Now
