package datasource

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

// cwd 级回连凭证：给没有会话记录的挂载方（discord 子区）用的
// /api/mcp/db/{token} 凭证。会话那套 token 落库且绑会话 id，这套只存
// 内存、绑工作目录——同一 cwd 复用同一枚，进程重启全部失效，挂载方
// 每次开会话现领即可。HandleMCP 解析时先查会话表，查不到再查这里。

type cwdTokens struct {
	mu      sync.Mutex
	byToken map[string]string // token → cwd
	byCwd   map[string]string // cwd → token（同 cwd 复用，防无限膨胀）
}

// issue 为一个 cwd 领取（或复用）一枚凭证。
func (c *cwdTokens) issue(cwd string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byCwd == nil {
		c.byCwd = map[string]string{}
		c.byToken = map[string]string{}
	}
	if t, ok := c.byCwd[cwd]; ok {
		return t, nil
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 cwd 凭证: %w", err)
	}
	t := "cwd_" + hex.EncodeToString(buf)
	c.byCwd[cwd] = t
	c.byToken[t] = cwd
	return t, nil
}

// lookup 反查凭证对应的 cwd。
func (c *cwdTokens) lookup(token string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cwd, ok := c.byToken[token]
	return cwd, ok
}
