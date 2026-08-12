package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// StringSlice 以 JSON 文本形式存进 sqlite 的字符串数组。
type StringSlice []string

func (s StringSlice) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	b, err := json.Marshal([]string(s))
	return string(b), err
}

func (s *StringSlice) Scan(src any) error {
	b, err := toBytes(src)
	if err != nil {
		return fmt.Errorf("scan StringSlice: %w", err)
	}
	if len(b) == 0 {
		*s = nil
		return nil
	}
	return json.Unmarshal(b, (*[]string)(s))
}

// StringMap 以 JSON 文本形式存进 sqlite 的字符串字典（如环境变量）。
type StringMap map[string]string

func (m StringMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	b, err := json.Marshal(map[string]string(m))
	return string(b), err
}

func (m *StringMap) Scan(src any) error {
	b, err := toBytes(src)
	if err != nil {
		return fmt.Errorf("scan StringMap: %w", err)
	}
	if len(b) == 0 {
		*m = nil
		return nil
	}
	return json.Unmarshal(b, (*map[string]string)(m))
}

// AgentModel 是探测缓存下来的一个可用模型（与 acp.Model 字段对齐，
// model 包不依赖 acp，由 service 层转换）。
// Disabled 是用户在配置页的取舍——零值即启用，旧缓存数据天然兼容。
type AgentModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

// AgentModelSlice 以 JSON 文本形式存进 sqlite 的模型清单。
type AgentModelSlice []AgentModel

func (s AgentModelSlice) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	b, err := json.Marshal([]AgentModel(s))
	return string(b), err
}

func (s *AgentModelSlice) Scan(src any) error {
	b, err := toBytes(src)
	if err != nil {
		return fmt.Errorf("scan AgentModelSlice: %w", err)
	}
	if len(b) == 0 {
		*s = nil
		return nil
	}
	return json.Unmarshal(b, (*[]AgentModel)(s))
}

// AgentCommand 是探测缓存下来的一条斜杠命令（与 acp.Command 字段对齐）。
// Disabled 是用户在配置页的取舍——零值即启用，旧缓存数据天然兼容。
type AgentCommand struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

// AgentSkeleton 是探测缓存的统一设置骨架：除模型外的维度清单与开关
// 支持位（与 acp.Settings 对齐，model 包不依赖 acp，由 service 层转换）。
// 未连接的会话靠它拼出与连接时结构一致的设置视图。
type AgentSkeleton struct {
	Efforts       []string `json:"efforts,omitempty"`
	Levels        []string `json:"levels,omitempty"`
	PlanSupported bool     `json:"planSupported,omitempty"`
	FastSupported bool     `json:"fastSupported,omitempty"`
}

func (s AgentSkeleton) Value() (driver.Value, error) {
	b, err := json.Marshal(s)
	return string(b), err
}

func (s *AgentSkeleton) Scan(src any) error {
	b, err := toBytes(src)
	if err != nil {
		return fmt.Errorf("scan AgentSkeleton: %w", err)
	}
	if len(b) == 0 {
		*s = AgentSkeleton{}
		return nil
	}
	return json.Unmarshal(b, s)
}

// AgentCommandSlice 以 JSON 文本形式存进 sqlite 的斜杠命令清单。
type AgentCommandSlice []AgentCommand

func (s AgentCommandSlice) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	b, err := json.Marshal([]AgentCommand(s))
	return string(b), err
}

func (s *AgentCommandSlice) Scan(src any) error {
	b, err := toBytes(src)
	if err != nil {
		return fmt.Errorf("scan AgentCommandSlice: %w", err)
	}
	if len(b) == 0 {
		*s = nil
		return nil
	}
	return json.Unmarshal(b, (*[]AgentCommand)(s))
}

// JSONMap 存放结构不固定的载荷，例如 ACP 的 tool_call 参数。
type JSONMap map[string]any

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	b, err := json.Marshal(map[string]any(m))
	return string(b), err
}

func (m *JSONMap) Scan(src any) error {
	if src == nil {
		*m = nil
		return nil
	}
	b, err := toBytes(src)
	if err != nil {
		return fmt.Errorf("scan JSONMap: %w", err)
	}
	if len(b) == 0 {
		*m = nil
		return nil
	}
	return json.Unmarshal(b, (*map[string]any)(m))
}

func toBytes(src any) ([]byte, error) {
	switch v := src.(type) {
	case nil:
		return nil, nil
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("unsupported source type %T", src)
	}
}
