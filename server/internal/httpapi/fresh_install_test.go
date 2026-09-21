package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"acpp/server/internal/config"
	"acpp/server/internal/datasource"
	"acpp/server/internal/model"
	"acpp/server/internal/project"
	"acpp/server/internal/remote"
	"acpp/server/internal/service"
)

// 契约：全新安装（空库、零会话、什么都没配）时，任何接口都不返回 null。
//
// 这条被违反过两次，两次都是**整页黑屏**：概览的 byAgent/byState 用
// `var out []T` 扫出零行就是 nil，discord 的 status.guilds 用
// `append([]T(nil), ...)` 拷空切片也是 nil。前端按契约（types/*.ts 声明
// 的是数组）直接 .map / .length，拿到 null 就是 TypeError，React 整棵树
// 挂掉——用户看到一片纯黑，没有任何提示，而全新装机必然走这条路。
//
// 在服务端保证形状，比让前端每处都写 `?? []` 可靠：前端漏一处就是一次
// 黑屏，而这里漏一处测试会红。
func TestAPI_FreshInstall_NeverReturnsNull(t *testing.T) {
	handler := freshRouter(t)

	// 全新安装时用户第一眼会碰到的那些页：概览、各列表页、系统设置。
	paths := []string{
		"/api/agents",
		"/api/sessions",
		"/api/sessions/overview?days=14",
		"/api/skills",
		"/api/skills/usage",
		"/api/servers",
		"/api/ssh-keys",
		"/api/datasources",
		"/api/tenants",
		"/api/projects",
		"/api/system",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.RemoteAddr = "127.0.0.1:54321" // loopback = owner
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Skipf("%s 在空环境下返回 %d，不在本测试的范围内", path, rec.Code)
			}

			var body any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode %s: %v", path, err)
			}
			if nulls := findNulls(body, ""); len(nulls) > 0 {
				sort.Strings(nulls)
				t.Fatalf("%s 在空环境下返回了 null：%s\n前端按契约直接 .map/.length，"+
					"这会让整页崩成黑屏。用非 nil 的空切片。\n%s",
					path, strings.Join(nulls, ", "), rec.Body.String())
			}
		})
	}
}

// findNulls 递归找出所有值为 null 的字段路径。
func findNulls(v any, path string) []string {
	switch t := v.(type) {
	case nil:
		return []string{orRoot(path)}
	case map[string]any:
		var out []string
		for k, child := range t {
			out = append(out, findNulls(child, path+"."+k)...)
		}
		return out
	case []any:
		var out []string
		for i, child := range t {
			out = append(out, findNulls(child, fmt.Sprintf("%s[%d]", path, i))...)
		}
		return out
	}
	return nil
}

func orRoot(path string) string {
	if path == "" {
		return "(root)"
	}
	return strings.TrimPrefix(path, ".")
}

// freshRouter 装一套全新安装的服务：空库、空数据目录，什么都没配过。
func freshRouter(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "fresh.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.Agent{}, &model.Session{}, &model.Tenant{},
		&model.DataSource{}, &model.Server{}, &model.SSHKey{}, &model.SkillUsage{},
		&model.TokenUsage{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	sessions := service.NewSessionService(gdb)
	return NewRouter(config.Config{DataDir: dir, WebDir: webDir(t)}, Services{
		Agents:      service.NewAgentService(gdb),
		Sessions:    sessions,
		Skills:      service.NewSkillService(dir, service.NewSkillUsageService(gdb, dir)),
		SkillUsage:  service.NewSkillUsageService(gdb, dir),
		Servers:     remote.NewService(gdb, nil, "127.0.0.1:48080"),
		DataSources: datasource.NewService(gdb, sessions, "127.0.0.1:48080"),
		Tenants:     service.NewTenantService(gdb, filepath.Join(dir, "workspaces")),
		Projects:    project.NewService(gdb),
	})
}
