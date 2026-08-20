package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// 契约：tools/call 每次都要交给 OnCall——不管工具成功还是失败。
// 观测漏掉失败调用，工具台上就只剩下好消息，排查时最想看的那一半没了。
func TestServeObservesEveryCall(t *testing.T) {
	tests := []struct {
		name        string
		tool        string
		call        func(context.Context, json.RawMessage) (string, error)
		wantResult  string
		wantIsError bool
	}{
		{
			name:       "成功调用记下返回文本",
			tool:       "ok_tool",
			call:       func(context.Context, json.RawMessage) (string, error) { return "3 行", nil },
			wantResult: "3 行",
		},
		{
			name:        "工具级错误同样记下，且记的是错误原话",
			tool:        "bad_tool",
			call:        func(context.Context, json.RawMessage) (string, error) { return "", errors.New("数据源 dev 只读") },
			wantResult:  "数据源 dev 只读",
			wantIsError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []Call
			srv := Server{
				Name: "acpp-db",
				Resolve: func(context.Context, string) ([]Tool, error) {
					return []Tool{{Name: tt.tool, Call: tt.call}}, nil
				},
				OnCall: func(_ context.Context, rec Call) { got = append(got, rec) },
			}

			req := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call",` +
				`"params":{"name":"` + tt.tool + `","arguments":{"sql":"select 1"}}}`)
			resp, hasResp := srv.Serve(context.Background(), "tok", req)
			if !hasResp {
				t.Fatal("tools/call 必须有响应")
			}

			if len(got) != 1 {
				t.Fatalf("OnCall 调用次数 = %d, want 1", len(got))
			}
			rec := got[0]
			if rec.Tool != tt.tool || rec.Server != "acpp-db" || rec.Token != "tok" {
				t.Errorf("记录归属错了: %+v", rec)
			}
			if rec.Result != tt.wantResult || rec.IsError != tt.wantIsError {
				t.Errorf("记录内容 = (%q, %v), want (%q, %v)",
					rec.Result, rec.IsError, tt.wantResult, tt.wantIsError)
			}
			if string(rec.Args) != `{"sql":"select 1"}` {
				t.Errorf("参数没原样记下: %s", rec.Args)
			}

			// 响应本身也要与观测一致：模型读到的 isError 就是记录里的那个。
			result := resp.(Response).Result.(map[string]any)
			if _, marked := result["isError"]; marked != tt.wantIsError {
				t.Errorf("响应 isError 标记 = %v, want %v", marked, tt.wantIsError)
			}
		})
	}
}

// 契约：没挂 OnCall 也要照常工作。观测是旁路，不是依赖。
func TestServeWithoutObserver(t *testing.T) {
	srv := Server{
		Name: "acpp-db",
		Resolve: func(context.Context, string) ([]Tool, error) {
			return []Tool{{Name: "t", Call: func(context.Context, json.RawMessage) (string, error) {
				return "done", nil
			}}}, nil
		},
	}
	resp, hasResp := srv.Serve(context.Background(),
		"", []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"t"}}`))
	if !hasResp {
		t.Fatal("应有响应")
	}
	if resp.(Response).Error != nil {
		t.Fatalf("不该报错: %+v", resp.(Response).Error)
	}
}

// 契约：tools/list 与工具台读的 Declare 是同一份声明——页面上看到的
// 描述与参数，必须就是模型看到的那一份。
func TestDeclareIsWhatToolsListReturns(t *testing.T) {
	tools := []Tool{{
		Name:        "db_query",
		Description: "只读查询",
		InputSchema: map[string]any{"type": "object"},
		Annotations: &Annotations{ReadOnlyHint: true},
	}}
	srv := Server{
		Name:    "acpp-db",
		Resolve: func(context.Context, string) ([]Tool, error) { return tools, nil },
	}

	resp, _ := srv.Serve(context.Background(), "", []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	listed := resp.(Response).Result.(map[string]any)["tools"]

	want := Declare(tools)
	gotJSON, _ := json.Marshal(listed)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("tools/list = %s, Declare = %s", gotJSON, wantJSON)
	}
	if !want[0].Annotations.ReadOnlyHint {
		t.Error("只读注解丢了")
	}
}

// 契约：resolve 失败一律同一个错，不泄露「这个 token 不存在」还是
// 「存在但没权限」——token 即凭证，区分了就是给人试探的余地。
func TestServeHidesResolveReason(t *testing.T) {
	for _, err := range []error{errors.New("not found"), errors.New("forbidden")} {
		srv := Server{
			Name:    "acpp-db",
			Resolve: func(context.Context, string) ([]Tool, error) { return nil, err },
		}
		resp, _ := srv.Serve(context.Background(), "x", []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		got := resp.(Response).Error
		if got == nil || got.Message != "unknown mcp endpoint" {
			t.Fatalf("错误信息 = %+v, 不该泄露原因", got)
		}
	}
}
