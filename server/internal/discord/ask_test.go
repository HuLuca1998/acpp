package discord

import "testing"

// 契约：一个子区能同时挂着多张问答卡，互不覆盖；裁决其中一张，其余的
// 照旧等着。agent 会并发发出多个权限请求（真机抓到过一轮里两个
// request_permission 前后脚到）——早先这里是单值，后到的会把先到的静默
// 顶掉：被顶掉那张卡还留在频道里，按钮点了只报「已失效」，而它对应的
// 工具调用永远等不到裁决，整轮就此卡死（后端的权限等待没有超时出口）。
func TestConcurrentAsksDoNotEvictEachOther(t *testing.T) {
	s := &Service{chats: map[string]*threadChat{}}
	const thread = "t1"
	tc := s.chatState(thread)
	first := &pendingAsk{kind: "permission", id: "p1", nonce: "n1", title: "Read A"}
	second := &pendingAsk{kind: "permission", id: "p2", nonce: "n2", title: "Read B"}
	tc.putAsk(first)
	tc.putAsk(second)

	if got := s.currentAsk(thread, "n1"); got != first {
		t.Fatalf("先到的那张卡取不回来了（被后到的顶掉）：%v", got)
	}
	if got := s.currentAsk(thread, "n2"); got != second {
		t.Fatalf("后到的那张卡取不回来：%v", got)
	}
	// 两张同时挂着时不接受「回编号」作答——编号指不明白是哪一张。
	if ask, n := tc.askForAnswer(); ask != nil || n != 2 {
		t.Errorf("askForAnswer = %v/%d, want nil/2", ask, n)
	}

	// 裁决掉一张，另一张必须原封不动地留着等裁决。
	s.clearAsk(thread, second)
	if got := s.currentAsk(thread, "n2"); got != nil {
		t.Errorf("裁决过的卡还在：%v", got)
	}
	if got := s.currentAsk(thread, "n1"); got != first {
		t.Errorf("裁决一张把另一张也带走了：%v", got)
	}
	// 只剩一张了，消息作答重新可用。
	if ask, n := tc.askForAnswer(); ask != first || n != 1 {
		t.Errorf("askForAnswer = %v/%d, want first/1", ask, n)
	}
}

// 契约：别处收口（网页点了、超时了）报的是 permission/elicitation 的 id，
// 得按 id 反查到对应那张卡并只清它——按 nonce 索引之后这条容易漏。
func TestAskDoneClearsOnlyMatchingCard(t *testing.T) {
	s := &Service{chats: map[string]*threadChat{}}
	tc := s.chatState("t1")
	keep := &pendingAsk{kind: "permission", id: "p1", nonce: "n1"}
	gone := &pendingAsk{kind: "permission", id: "p2", nonce: "n2"}
	tc.putAsk(keep)
	tc.putAsk(gone)

	// msgID 为空时 askDone 不发 REST，正好只验状态清理。
	s.askDone("", "t1", tc, "p2")

	if got := s.currentAsk("t1", "n2"); got != nil {
		t.Errorf("id 命中的卡没被清掉：%v", got)
	}
	if got := s.currentAsk("t1", "n1"); got != keep {
		t.Errorf("不相干的卡被清掉了：%v", got)
	}
}
