package fswatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 等一声变动信号：合帧窗口之外留足余量，慢机器上也不该假失败。
const waitFor = 3 * time.Second

func waitSignal(t *testing.T, ch <-chan struct{}) bool {
	t.Helper()
	select {
	case _, ok := <-ch:
		return ok
	case <-time.After(waitFor):
		return false
	}
}

func TestWatchReportsFileChange(t *testing.T) {
	root := t.TempDir()
	events, stop, err := Watch(root)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer stop()

	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !waitSignal(t, events) {
		t.Fatal("新建文件没有触发变动信号")
	}
}

// 新建的目录必须自动纳入监视：「先建目录再往里写文件」是最常见的一种
// 改动，漏了它等于这一支的改动永远看不见。
func TestWatchFollowsNewDirectories(t *testing.T) {
	root := t.TempDir()
	events, stop, err := Watch(root)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer stop()

	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if !waitSignal(t, events) {
		t.Fatal("新建目录没有触发变动信号")
	}

	if err := os.WriteFile(filepath.Join(sub, "b.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !waitSignal(t, events) {
		t.Fatal("新建目录里的文件没有触发变动信号")
	}
}

// 排除目录里的动静不算改动：`.git` 每跑一条 git 命令就写几十次，
// `node_modules` 装一次依赖能刷屏——它们进来只会让面板空转。
// 递归监视没有「不进这个目录」的开关，过滤发生在事件这一侧。
func TestWatchIgnoresSkippedDirs(t *testing.T) {
	root := t.TempDir()
	git := filepath.Join(root, ".git")
	if err := os.Mkdir(git, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	events, stop, err := Watch(root)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer stop()

	if err := os.WriteFile(filepath.Join(git, "index"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case <-events:
		t.Fatal(".git 里的写入不该触发变动信号")
	case <-time.After(debounce * 3):
	}
}

// 成串的改动合成一声：保存一次文件在文件系统那边就是好几个事件，
// 面板要的是「这阵子过去之后重读一遍」。
func TestWatchDebouncesBurst(t *testing.T) {
	root := t.TempDir()
	events, stop, err := Watch(root)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer stop()

	for i := range 20 {
		name := filepath.Join(root, "f"+string(rune('a'+i))+".txt")
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if !waitSignal(t, events) {
		t.Fatal("成串写入没有触发变动信号")
	}
	// 合帧之后应当安静下来，而不是一个事件补一声。
	select {
	case <-events:
		t.Fatal("一串写入只该合成一声信号")
	case <-time.After(debounce * 3):
	}
}

func TestStopClosesChannel(t *testing.T) {
	root := t.TempDir()
	events, stop, err := Watch(root)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	stop()
	stop() // 重复关闭必须安全

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("停掉之后 channel 该关闭，而不是继续发信号")
		}
	case <-time.After(waitFor):
		t.Fatal("停掉之后 channel 没有关闭")
	}
}

// Hub 的契约：同一个目录共用一个监视器，每个订阅者各收各的信号；
// 最后一个退订之后监视器收摊。
func TestHubFansOutAndReleases(t *testing.T) {
	root := t.TempDir()
	hub := NewHub()

	a, cancelA, err := hub.Subscribe(root)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	b, cancelB, err := hub.Subscribe(root)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if got := len(hub.roots); got != 1 {
		t.Fatalf("同一个目录该共用一个监视器，实际 %d 个", got)
	}

	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !waitSignal(t, a) {
		t.Fatal("订阅者 a 没收到信号")
	}
	if !waitSignal(t, b) {
		t.Fatal("订阅者 b 没收到信号")
	}

	cancelA()
	if got := len(hub.roots); got != 1 {
		t.Fatalf("还有人看着，监视器不该收摊（剩 %d 项）", got)
	}
	cancelB()
	cancelB() // 重复退订必须安全
	if got := len(hub.roots); got != 0 {
		t.Fatalf("没人看了监视器该收摊，实际还剩 %d 项", got)
	}
}
