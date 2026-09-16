// Package fswatch 监视一个工作目录的文件变动，把成串的事件合成一声「变了」。
//
// 存在的理由：工作区面板此前只在 agent 干完一件事之后刷新，用户自己在
// 编辑器里改的、命令行里跑脚本生成的，界面一概不知道，看到的是旧内容。
// 这里补的就是那条路——改动来自谁都算数。
//
// 只说「变了」，不说「谁变了」：面板本来就要整片重读（文件树、git 汇总、
// 正在看的那个文件），逐个路径推送对它们没有用处，反而要多一份状态。
//
// 用 rjeczalik/notify 而不是更常见的 fsnotify，是被实际的工作目录逼出来的：
// fsnotify 在 macOS 上走 kqueue，**一个目录一个文件句柄，而且目录里的每个
// 文件也要一个**。实测一个工作区根目录有 1.3 万个目录、7 万个文件——光是
// 建立监视就要打开八万多个句柄，开销与耗时都不可接受。notify 在 darwin 上
// 走 FSEvents：整棵树一个流，与规模无关。代价是递归流会把排除目录里的
// 动静也送过来，得在事件这一侧过滤（见 skipPath）。
package fswatch

import (
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rjeczalik/notify"
)

// 合帧窗口。保存一个文件、切一次分支、跑一次构建，在文件系统那边都是
// 几十上百个事件；界面要的是「这阵子过去之后重读一遍」，不是每个事件
// 都跟一次。窗口内再来事件就顺延，直到安静下来才发信号。
const debounce = 400 * time.Millisecond

// 原始事件的缓冲。构建产物落盘能在一瞬间刷出成千上万个事件，满了就丢是
// 对的——我们只要知道「有动静」，丢掉的那些不改变结论。
const eventBuffer = 256

// 不算数的目录名。它们要么是工具的自留地（`.git` 的索引与锁文件每跑一条
// git 命令就写几十次），要么是产物与依赖（`node_modules` 装一次能刷屏）
// ——放进来只会让面板空转。
//
// `.git` 被排除意味着 commit 这类只动版本库、不动工作区的操作不触发刷新，
// 那部分仍由 agent 干完活之后的广播覆盖，两者互补。
var skipDirs = map[string]bool{
	".git":          true,
	"node_modules":  true,
	"dist":          true,
	"build":         true,
	"target":        true,
	"vendor":        true,
	".next":         true,
	".nuxt":         true,
	".turbo":        true,
	".cache":        true,
	".venv":         true,
	"venv":          true,
	"__pycache__":   true,
	".pytest_cache": true,
	".gradle":       true,
	".idea":         true,
	".vscode":       true,
}

// skipPath 判断这条路径算不算数。递归监视拿不到「不进这个目录」的开关，
// 只能在事件这一侧看路径。
//
// 根目录**自身**的事件也不算：FSEvents 把「某个子项变了」报到父目录头上，
// 于是 `.git/index` 写一次，根目录跟着也来一条——只看排除名单的话，被挡掉
// 的动静会从这条绕回来，界面每跑一条 git 命令就空转一次。真有文件动了，
// 它自己那条路径的事件一定会来，所以丢掉根目录这条不会漏报。
func skipPath(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		if skipDirs[seg] {
			return true
		}
	}
	return false
}

// Watch 开始监视 root 下的文件变动（递归）。
//
// 返回的 channel 每次合帧窗口结束发一个信号（缓冲 1，读得慢就合并——
// 积压的「变了」没有意义，最新那一声就代表全部）。stop 关掉监视并关闭
// channel；重复调用安全。
//
// 监视建不起来（目录不存在、平台上的监视配额用尽）时返回错误，调用方
// 据此降级回手动刷新——那不是故障，是这一个目录暂时没法监视。
func Watch(root string) (<-chan struct{}, func(), error) {
	// 事件里的路径是内核给的**真实**路径，而调用方传进来的 root 可能经过
	// 软链（macOS 的 /var 就是 /private/var 的软链，临时目录全在它下面）。
	// 不先对齐，下面按相对路径做的排除判断会全部落空。
	base := root
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		base = resolved
	}

	raw := make(chan notify.EventInfo, eventBuffer)
	// "..." 是 notify 的递归写法：darwin 上落到 FSEvents 的一个流。
	if err := notify.Watch(filepath.Join(root, "..."), raw, notify.All); err != nil {
		return nil, nil, err
	}
	slog.Debug("fswatch started", "root", root)

	events := make(chan struct{}, 1)
	var once sync.Once
	stop := func() {
		once.Do(func() {
			notify.Stop(raw)
			// notify.Stop 不关闭 channel，pump 得有别的办法知道该收摊。
			close(raw)
		})
	}

	go pump(base, raw, events)
	return events, stop, nil
}

// pump 把原始事件合帧成信号，直到监视被停掉。
//
// 关闭 events 的责任在这里：只有这个 goroutine 知道自己不会再发了。
func pump(root string, raw <-chan notify.EventInfo, events chan<- struct{}) {
	defer close(events)

	// 停着的计时器：第一个事件到来才启动，之后每来一个就往后顺延。
	timer := time.NewTimer(debounce)
	if !timer.Stop() {
		<-timer.C
	}
	pending := false

	for {
		select {
		case ev, ok := <-raw:
			if !ok {
				return
			}
			if skipPath(root, ev.Path()) {
				continue
			}
			if pending && !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(debounce)
			pending = true
		case <-timer.C:
			pending = false
			select {
			case events <- struct{}{}:
			default:
				// 上一声还没被取走，合并即可。
			}
		}
	}
}
