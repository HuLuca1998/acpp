package remote

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"acpp/server/internal/model"
)

const (
	// cmdTimeout 是单条命令的上限。观察类命令都该秒回，卡住多半是对端
	// 有问题，等下去只会把会话也拖住。
	cmdTimeout = 30 * time.Second
	// outBudget 是一次工具调用能带回的字节数上限（约 8k token）。
	// 超出在**服务端**就截断——只在返回值上截断的话，服务器那边已经白干了。
	outBudget = 32 * 1024
	// truncMark 是截断提示，跟着输出一起给模型看，好让它知道要收窄条件。
	truncMark = "\n…（输出已截断到 %s，用 offset / tail / grep 收窄范围）"
)

// shellQuote 把一个值包成 shell 单引号字面量。
//
// **这是本功能唯一的注入面**：SSH 的 exec 请求只接受一个命令字符串，由对端
// shell 解析——路径、pattern、容器名全都是模型给的，一律经过这里，绝不做
// 字符串拼接。单引号内除了单引号自身没有任何元字符，所以只需把 `'` 换成
// `'\”`（闭合、转义一个单引号、再打开）。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// quoteAll 是 shellQuote 的批量版，拼命令行时用。
func quoteAll(args ...string) string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = shellQuote(a)
	}
	return strings.Join(out, " ")
}

// execResult 是一次远程命令的结果。
type execResult struct {
	Out string
	// Truncated 表示输出撞到了预算上限。
	Truncated bool
	// Code 是退出码。命令自身失败（找不到文件、没装 docker）不算错误——
	// 那是要如实转述给模型的信息，不是我们这一侧的故障。
	Code int
}

// run 在一台服务器上跑一条只读命令。
//
// 连接是一次性的（照 adr-008 的口径）：拨号 → 执行 → 关闭，不做连接池。
// 排障时模型会连调十几次，每次握手 200–500ms 是实打实的等待，但复用要处理
// 探活、并发与失效，先按简单的来，实测慢了再说。
func (s *Service) run(ctx context.Context, srv *model.Server, cmd string) (execResult, error) {
	ctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()

	client, err := sshdialDial(ctx, srv)
	if err != nil {
		return execResult{}, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return execResult{}, fmt.Errorf("开 ssh 会话失败: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	// 多留一点余量再截断：正好卡在预算上时也能看出「确实还有」。
	session.Stdout = &limitWriter{w: &stdout, n: outBudget}
	session.Stderr = &limitWriter{w: &stderr, n: 4 * 1024}

	done := make(chan error, 1)
	go func() { done <- session.Run(cmd) }()

	select {
	case <-ctx.Done():
		// 对端还在跑：给它一个信号再撒手，免得留一条命令在生产机上空转。
		_ = session.Signal(ssh.SIGKILL)
		return execResult{}, fmt.Errorf("命令超时（超过 %s）", cmdTimeout)
	case err := <-done:
		res := execResult{
			Out:       stdout.String(),
			Truncated: stdout.Len() >= outBudget,
		}
		if err != nil {
			var exit *ssh.ExitError
			if ok := asExitError(err, &exit); ok {
				res.Code = exit.ExitStatus()
				// 退出码非 0 时 stderr 才是有用信息（「没有那个文件」之类）。
				if msg := strings.TrimSpace(stderr.String()); msg != "" {
					res.Out = strings.TrimRight(res.Out, "\n")
					if res.Out != "" {
						res.Out += "\n"
					}
					res.Out += msg
				}
				return res, nil
			}
			return execResult{}, fmt.Errorf("执行失败: %w", err)
		}
		return res, nil
	}
}

// ansiRe 匹配终端颜色/光标转义序列。
//
// 服务端日志里这东西很常见（zap 的 console 编码器默认给日志级别上色），
// 对模型纯属噪音：既占 token 又让 `error` 这样的词变成 `\x1b[31merror\x1b[0m`
// 而搜不到。在我们这侧剥而不是让远端 sed 做——BSD 与 GNU 的 sed 对 \x1b
// 的写法不一样，靠远端处理会在某些机器上悄悄失效。
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// text 是 run 的便捷版：直接给能交给模型的文本，带截断提示。
func (s *Service) text(ctx context.Context, srv *model.Server, cmd string) (string, error) {
	res, err := s.run(ctx, srv, cmd)
	if err != nil {
		return "", err
	}
	out := strings.TrimRight(ansiRe.ReplaceAllString(res.Out, ""), "\n")
	if res.Truncated {
		out += fmt.Sprintf(truncMark, humanBytes(outBudget))
	}
	if strings.TrimSpace(out) == "" {
		return "（没有输出）", nil
	}
	return out, nil
}

// limitWriter 在写满 n 字节后丢弃剩余内容——**服务端的活也一起省下**是靠
// 命令里的 head/tail 限流，这里只是最后一道兜底，防止对端不认那些参数。
type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil
	}
	if len(p) > l.n {
		_, err := l.w.Write(p[:l.n])
		l.n = 0
		return len(p), err
	}
	l.n -= len(p)
	return l.w.Write(p)
}

// nice 给命令降优先级。生产机的负载常年不低，一次莽撞的全盘 grep 能把
// 正经服务挤出去——观察类工具没有任何理由跟业务抢 CPU。
func nice(cmd string) string {
	return "nice -n 19 " + cmd
}

func humanBytes(n int) string {
	if n >= 1024 {
		return strconv.Itoa(n/1024) + "KB"
	}
	return strconv.Itoa(n) + "B"
}
