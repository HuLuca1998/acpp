package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

// PeerTokens 是非会话调用方（discord 子区这类没有会话记录的挂载方）的
// 回连凭证。会话那套 token 落库且绑会话 id，这套只存内存、绑
// (调用方 key, 工作目录, 作用域)——同一 key 复用同一枚，进程重启全部失效，
// 挂载方每次开会话现领即可。各工具面（datasource / report）各持一份。
type PeerTokens struct {
	mu      sync.Mutex
	byToken map[string]peer // token → (key, cwd, scope)
	byKey   map[string]string
}

type peer struct {
	key, cwd string
	scope    uint
}

// Issue 为一个调用方领取（或复用）一枚凭证。cwd 或 scope 变了（同 key
// 重绑到别的目录/别的作用域）会换发新凭证，旧凭证随之作废。
//
// scope 是调用方自定义的作用域标识，工具面各自解释（datasource 用它记
// 「这枚凭证锁定的数据源 id」）；不需要就传 0。它必须参与凭证的身份，
// 否则两个 cwd 相同、作用域不同的调用方会互相踢掉对方的凭证。
func (p *PeerTokens) Issue(key, cwd string, scope uint) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.byKey == nil {
		p.byKey = map[string]string{}
		p.byToken = map[string]peer{}
	}
	if t, ok := p.byKey[key]; ok {
		if cur := p.byToken[t]; cur.cwd == cwd && cur.scope == scope {
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
	p.byToken[t] = peer{key: key, cwd: cwd, scope: scope}
	return t, nil
}

// Lookup 反查凭证对应的 (key, cwd, scope)。
func (p *PeerTokens) Lookup(token string) (key, cwd string, scope uint, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pe, ok := p.byToken[token]
	return pe.key, pe.cwd, pe.scope, ok
}
