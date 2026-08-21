package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"acpp/server/internal/config"
)

// 心跳间隔。空闲的长连接需要定期有字节流动：一是让服务端尽早发现对端
// 已经走了（写失败即收摊），二是避免中间层按空闲超时把连接掐断。
const eventsHeartbeat = 25 * time.Second

// 断线重连间隔（毫秒），随流开头的 retry 指令下发。浏览器默认 3 秒，
// 这里调快——这条流断了基本就等于后端在重启，早一秒连上就早一秒提示。
const eventsRetryMS = 1500

// serverEvents 是与会话无关的全局事件流：连上就先收到一条 hello 带当前
// 后端版本，之后只有心跳。
//
// 它靠「进程换了，这条流必断」工作，而不是靠轮询：owner 的一键更新会替换
// .app 并重启后端，所有在线页面的这条流随之断开，浏览器自行重连，重连后
// 拿到的版本与手里那份对不上，就说明该刷新了。局域网访客因此能在后端起来
// 后一两秒内收到提示，而不是等下一次轮询——轮询的发现延迟下限就是轮询
// 间隔，要做到秒级就得让每个页面每秒打一次 health。
func serverEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, envelope{Error: "streaming unsupported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// 关掉 nginx 一类反代的缓冲，否则 SSE 会被攒起来一次性下发。
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	hello, err := json.Marshal(map[string]string{"kind": "hello", "version": config.Version})
	if err != nil {
		return
	}
	if _, err := fmt.Fprintf(w, "retry: %d\ndata: %s\n\n", eventsRetryMS, hello); err != nil {
		return
	}
	flusher.Flush()

	ticker := time.NewTicker(eventsHeartbeat)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// SSE 注释行：只为保活，不会触发前端的 onmessage。
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
