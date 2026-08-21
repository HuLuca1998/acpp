package stream

import (
	"testing"
	"time"
)

// 通知的归属是硬边界：租户之间互不可见，owner 也不该被别人的会话刷屏
// （Publish 的注释解释了为什么 owner 不是「收全部」）。
func TestHubDeliversOnlyToOwningTenant(t *testing.T) {
	hub := NewHub()

	owner, cancelOwner := hub.Subscribe(0)
	defer cancelOwner()
	alice, cancelAlice := hub.Subscribe(7)
	defer cancelAlice()
	bob, cancelBob := hub.Subscribe(9)
	defer cancelBob()

	hub.Publish(Notice{Kind: "notify", Event: "permission", SessionID: 1, TenantID: 0})
	hub.Publish(Notice{Kind: "notify", Event: "elicitation", SessionID: 2, TenantID: 7})

	cases := []struct {
		name    string
		ch      <-chan Notice
		want    string
		wantSID uint
	}{
		{"owner 收自己的", owner, "permission", 1},
		{"租户收自己的", alice, "elicitation", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			select {
			case got := <-tc.ch:
				if got.Event != tc.want || got.SessionID != tc.wantSID {
					t.Fatalf("收到 %+v，想要 event=%s sessionId=%d", got, tc.want, tc.wantSID)
				}
			case <-time.After(time.Second):
				t.Fatal("该收到通知却没收到")
			}
		})
	}

	// 谁的都不是的那位必须一条都收不到。
	select {
	case got := <-bob:
		t.Fatalf("租户 9 收到了不属于他的通知：%+v", got)
	default:
	}

	// 各自只有一条，不该串台。
	for name, ch := range map[string]<-chan Notice{"owner": owner, "alice": alice} {
		select {
		case got := <-ch:
			t.Fatalf("%s 多收到一条：%+v", name, got)
		default:
		}
	}
}

// 退订后广播不该再往已关闭的订阅上投递——那是一次 panic，会把整条
// 广播链路带走。
func TestHubPublishAfterCancel(t *testing.T) {
	hub := NewHub()
	ch, cancel := hub.Subscribe(3)
	cancel()

	if _, ok := <-ch; ok {
		t.Fatal("退订后 channel 应已关闭")
	}
	hub.Publish(Notice{Kind: "notify", Event: "error", TenantID: 3})
}

// 慢订阅者不能拖住广播：缓冲满了就丢，Publish 必须立刻返回。
// 迟到的打扰没有补发价值，卡住广播却会连累所有人。
func TestHubDropsInsteadOfBlocking(t *testing.T) {
	hub := NewHub()
	_, cancel := hub.Subscribe(1)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < noticeBuffer*3; i++ {
			hub.Publish(Notice{Kind: "notify", Event: "turn_end", TenantID: 1})
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish 被慢订阅者卡住了")
	}
}

// 关停必须让订阅者立刻收到流结束，而不是留他们干等心跳；晚到的退订
// 不能把已关的 channel 再关一次（panic）。
func TestHubCloseEndsSubscribers(t *testing.T) {
	hub := NewHub()
	ch, cancel := hub.Subscribe(1)

	hub.Close()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("Close 后不该还能收到通知")
		}
	case <-time.After(time.Second):
		t.Fatal("Close 后订阅 channel 应立即关闭")
	}
	cancel()                                         // 不该 panic
	hub.Publish(Notice{Kind: "notify", TenantID: 1}) // 也不该 panic

	// 关停后的新订阅同样立刻结束。
	late, _ := hub.Subscribe(2)
	if _, ok := <-late; ok {
		t.Fatal("关停后的新订阅应拿到已关闭的 channel")
	}
}

// Broker 同款语义：Close 与订阅方 cancel 谁先谁关，另一方安静退场。
func TestBrokerCloseThenCancel(t *testing.T) {
	b := NewBroker()
	ch, cancel := b.Subscribe()

	b.Close()
	if _, ok := <-ch; ok {
		t.Fatal("Close 后订阅 channel 应已关闭")
	}
	cancel()                       // 不该 panic
	b.Publish(Event{Kind: "noop"}) // 不该 panic

	late, _ := b.Subscribe()
	if _, ok := <-late; ok {
		t.Fatal("关停后的新订阅应拿到已关闭的 channel")
	}
}
