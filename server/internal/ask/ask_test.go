package ask

import (
	"testing"

	"acpp/server/internal/model"
)

// 契约：/api/ask 交回的 text 是「最后一条提问之后 agent 说的全部正文」——
// 工具调用把正文切成的几段按序拼起来，思考与工具卡不算，更早轮次的回答不算。
func TestReplyAfterLastPrompt(t *testing.T) {
	agentText := func(s string) model.Message {
		return model.Message{Role: model.RoleAgent, Kind: model.KindText, Content: s}
	}
	user := func(s string) model.Message {
		return model.Message{Role: model.RoleUser, Kind: model.KindText, Content: s}
	}
	cases := []struct {
		name string
		all  []model.Message
		want string
	}{
		{"空转录", nil, ""},
		{"单轮", []model.Message{user("q"), agentText("a")}, "a"},
		{"只取最后一轮", []model.Message{user("q1"), agentText("a1"), user("q2"), agentText("a2")}, "a2"},
		{"跳过思考与工具、拼接分段", []model.Message{
			user("q"),
			{Role: model.RoleAgent, Kind: model.KindThought, Content: "hmm"},
			agentText("先看代码。"),
			{Role: model.RoleAgent, Kind: model.KindToolCall, Content: "read"},
			agentText("  结论：没问题。  "),
		}, "先看代码。\n\n结论：没问题。"},
		{"提问后还没回答", []model.Message{user("q1"), agentText("a1"), user("q2")}, ""},
		// 界面上的人插话后锚点滑到人的那条上，ask 自己那问的回答就取不到了——
		// 这正是 waitTurn 要在事件层拦截插话（ErrInterjected）而不是事后补救的原因。
		{"人插话后锚点滑走", []model.Message{user("q1"), agentText("a1"), user("human")}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReplyAfterLastPrompt(tc.all); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
