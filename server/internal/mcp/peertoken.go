package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

// PeerTokens 是非会话调用方（discord 子区这类没有会话记录的挂载方）的
// 回连凭证。会话那套 token 落库且绑会话 id，这套只存内存、绑
// (调用方 key, 工作目录)——同一 key 复用同一枚，进程重启全部失效，
// 挂载方每次开会话现领即可。各工具面（datasource / report）各持一份。
type PeerTokens struct {
	mu      sync.Mutex
	byToken map[string]peer // token → (key, cwd)
	byKey   map[string]string
}

type peer struct{ key, cwd string }

// Issue 为一个调用方领取（或复用）一枚凭证。cwd 变了（同 key 重绑到
// 别的目录）会换发新凭证，旧凭证随之作废。
func (p *PeerTokens) Issue(key, cwd string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.byKey == nil {
		p.byKey = map[string]string{}
		p.byToken = map[string]peer{}
	}
	if t, ok := p.byKey[key]; ok {
		if p.byToken[t].cwd == cwd {
			return t, nil
		}
		delete(p.byToken, t)
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成回连凭证: %w", err)
	}
	t := "peer_" + hex.EncodeToString(buf)
	p.byKey[key] = t
	p.byToken[t] = peer{key: key, cwd: cwd}
	return t, nil
}

// Lookup 反查凭证对应的 (key, cwd)。
func (p *PeerTokens) Lookup(token string) (key, cwd string, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pe, ok := p.byToken[token]
	return pe.key, pe.cwd, ok
}
