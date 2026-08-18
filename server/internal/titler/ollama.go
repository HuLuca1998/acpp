package titler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ollama 的原生 /api/chat 而不是 OpenAI 兼容的 /v1/chat/completions：
// 实测带 thinking 的模型（qwen3 系列）在兼容端点上把答案全写进 reasoning
// 字段、content 返回空串，而关掉思考的 think 参数只有原生接口认。

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	// Think 关掉思考链。指针是为了区分「显式关闭」与「不带这个字段」——
	// 不支持思考的模型收到 think 会报错，探测不到就不传。
	Think   *bool           `json:"think,omitempty"`
	Options chatOptions     `json:"options"`
	Format  json.RawMessage `json:"format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatOptions struct {
	Temperature float64  `json:"temperature"`
	NumPredict  int      `json:"num_predict"`
	Stop        []string `json:"stop,omitempty"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
	Error   string      `json:"error,omitempty"`
}

// chat 发一轮对话并返回模型原文（未净化）。
func (s *Service) chat(ctx context.Context, sys, user string, think *bool) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model:  s.cfg.Model,
		Stream: false,
		Think:  think,
		Messages: []chatMessage{
			{Role: "system", Content: sys},
			{Role: "user", Content: user},
		},
		Options: chatOptions{Temperature: 0.2, NumPredict: 60, Stop: []string{"\n"}},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(s.cfg.BaseURL, "/")+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("调用 ollama 失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama 返回 %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("解析 ollama 响应失败: %w", err)
	}
	if out.Error != "" {
		return "", fmt.Errorf("ollama: %s", out.Error)
	}
	return out.Message.Content, nil
}

type tagsResponse struct {
	Models []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	} `json:"models"`
}

// Model 是一个可选模型，Size 供界面提示体积（标题这种轻活选小的更快）。
type Model struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// Models 拉某个 ollama 端点上已安装的模型清单。baseURL 由调用方传入而不是
// 读配置：设置页要在「还没保存」的地址上先试拉一把。
func Models(ctx context.Context, client *http.Client, baseURL string) ([]Model, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接 ollama 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama 返回 %d", resp.StatusCode)
	}
	var out tagsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("解析模型清单失败: %w", err)
	}
	models := make([]Model, 0, len(out.Models))
	for _, m := range out.Models {
		models = append(models, Model{Name: m.Name, Size: m.Size})
	}
	return models, nil
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
