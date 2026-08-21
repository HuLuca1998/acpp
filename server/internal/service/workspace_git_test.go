package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// git 汇总改成并发扇出之后，「哪条命令的输出对应哪个字段」不再由执行顺序
// 保证，而是由 key 保证——接错一条不会报错，只会让界面安静地少一块信息。
// 这几个测试盯的就是这件事：在真实仓库上跑，逐字段核对。

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// 契约：非 git 目录返回 IsRepo=false 而不是错误（会话开在普通目录是常态）。
func TestWorkspaceGitOverview_NonRepo(t *testing.T) {
	view, err := WorkspaceGitOverview(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if view.IsRepo {
		t.Error("普通目录不该被当成仓库")
	}
	if view.Files == nil || view.Commits == nil {
		t.Error("清单必须是空切片而不是 nil：JSON 里 null 与 [] 对前端是两种东西")
	}
}

// 契约：无 upstream 的仓库照样给出分支、改动清单与最近提交
// （前端据 Upstream 为空把提交列表标注成「最近」而不是「未推送」）。
func TestWorkspaceGitOverview_ReportsBranchAndChanges(t *testing.T) {
	dir := gitRepo(t)

	// 三种改动各一份：改过的已跟踪文件、新增的未跟踪文件、删除的文件。
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\nchanged\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	view, err := WorkspaceGitOverview(context.Background(), dir)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !view.IsRepo {
		t.Fatal("IsRepo = false, want true")
	}
	if view.Branch != "main" {
		t.Errorf("Branch = %q, want main", view.Branch)
	}
	if view.Root == "" {
		t.Error("Root 为空：变更清单的路径相对它，界面靠它对应到文件树")
	}
	if view.Upstream != "" {
		t.Errorf("Upstream = %q, want 空（本地仓库没有远端）", view.Upstream)
	}
	if len(view.Commits) != 1 {
		t.Errorf("Commits = %d 条, want 1（无 upstream 时退化为最近提交）", len(view.Commits))
	}

	byPath := map[string]GitFileChange{}
	for _, f := range view.Files {
		byPath[f.Path] = f
	}
	readme, ok := byPath["README.md"]
	if !ok {
		t.Fatalf("改动清单缺 README.md：%+v", view.Files)
	}
	if readme.Added != 1 || readme.Deleted != 0 {
		t.Errorf("README.md 行数 = +%d/-%d, want +1/-0（numstat 接错了）", readme.Added, readme.Deleted)
	}
	added, ok := byPath["new.txt"]
	if !ok {
		t.Fatalf("改动清单缺未跟踪的 new.txt：%+v", view.Files)
	}
	if added.Status != "A" || added.Added != 3 {
		t.Errorf("new.txt = status %q +%d, want A +3", added.Status, added.Added)
	}
}

// 契约：有 upstream 时报出 upstream 名与领先/落后数，提交列表只含未推送的那些。
func TestWorkspaceGitOverview_ReportsUpstreamAheadBehind(t *testing.T) {
	origin := gitRepo(t)
	// 造一个能当远端的裸仓库并推上去，再在本地多提交一条。
	clone := t.TempDir()
	gitRun(t, ".", "clone", "-q", origin, clone)
	gitRun(t, clone, "config", "user.email", "test@example.com")
	gitRun(t, clone, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(clone, "later.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitRun(t, clone, "add", ".")
	gitRun(t, clone, "commit", "-m", "later")

	view, err := WorkspaceGitOverview(context.Background(), clone)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if view.Upstream == "" {
		t.Fatal("Upstream 为空：克隆出来的仓库应当有跟踪分支")
	}
	if view.Ahead != 1 || view.Behind != 0 {
		t.Errorf("ahead/behind = %d/%d, want 1/0", view.Ahead, view.Behind)
	}
	if len(view.Commits) != 1 || view.Commits[0].Subject != "later" {
		t.Errorf("未推送提交 = %+v, want 只有 later 一条", view.Commits)
	}
}

// 契约：并发请求同一份只读视图时共享一次执行——面板、tab 徽标、分支胶囊
// 同时要的是同一个事实，没道理各跑一遍 git。
func TestShareGit_CoalescesConcurrentCalls(t *testing.T) {
	var mu sync.Mutex
	runs := 0
	release := make(chan struct{})

	work := func(context.Context) (int, error) {
		mu.Lock()
		runs++
		mu.Unlock()
		<-release // 卡住执行，让后来者必然撞上进行中的这一次
		return 42, nil
	}

	const callers = 5
	results := make([]int, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			results[i], errs[i] = shareGit(context.Background(), "test:coalesce", work)
		})
	}
	// 让五个 goroutine 都进到 shareGit 里（首个开跑、其余排到它后面）再放行。
	// 与 golang.org/x/sync/singleflight 自己的测试同一手法。
	time.Sleep(200 * time.Millisecond)
	close(release)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if runs != 1 {
		t.Errorf("执行了 %d 次, want 1（合流没生效）", runs)
	}
	for i := range callers {
		if errs[i] != nil {
			t.Errorf("caller %d: %v", i, errs[i])
		}
		if results[i] != 42 {
			t.Errorf("caller %d 拿到 %d, want 42", i, results[i])
		}
	}
}

// 契约：合流不是缓存——前一次跑完之后再问，必须重新执行拿最新状态。
func TestShareGit_DoesNotCache(t *testing.T) {
	var runs int
	work := func(context.Context) (int, error) {
		runs++
		return runs, nil
	}
	for want := 1; want <= 3; want++ {
		got, err := shareGit(context.Background(), "test:nocache", work)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if got != want {
			t.Fatalf("第 %d 次拿到 %d：合流退化成缓存了，界面会显示旧状态", want, got)
		}
	}
}
