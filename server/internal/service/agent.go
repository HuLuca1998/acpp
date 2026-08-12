package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"acpp/server/internal/model"
)

// ErrNotFound 表示目标记录不存在，由 HTTP 层翻译成 404。
var ErrNotFound = errors.New("not found")

// ErrInvalid 表示入参不合法，由 HTTP 层翻译成 400。
var ErrInvalid = errors.New("invalid input")

// AgentService 负责 agent 配置的读写。
type AgentService struct {
	db *gorm.DB
}

func NewAgentService(db *gorm.DB) *AgentService {
	return &AgentService{db: db}
}

// AgentInput 是创建/更新 agent 的入参。
type AgentInput struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	Cwd         string            `json:"cwd"`
	Status      string            `json:"status"`
}

func (in AgentInput) validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if strings.TrimSpace(in.Command) == "" {
		return fmt.Errorf("%w: command is required", ErrInvalid)
	}
	return nil
}

func (s *AgentService) List(ctx context.Context) ([]model.Agent, error) {
	var agents []model.Agent
	if err := s.db.WithContext(ctx).Order("id asc").Find(&agents).Error; err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	return agents, nil
}

func (s *AgentService) Get(ctx context.Context, id uint) (*model.Agent, error) {
	var agent model.Agent
	err := s.db.WithContext(ctx).First(&agent, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("agent %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get agent %d: %w", id, err)
	}
	return &agent, nil
}

func (s *AgentService) Create(ctx context.Context, in AgentInput) (*model.Agent, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}

	agent := model.Agent{
		Name:        strings.TrimSpace(in.Name),
		Description: in.Description,
		Command:     strings.TrimSpace(in.Command),
		Args:        in.Args,
		Env:         in.Env,
		Cwd:         in.Cwd,
		Status:      model.AgentIdle,
	}
	if in.Status != "" {
		agent.Status = model.AgentStatus(in.Status)
	}

	if err := s.db.WithContext(ctx).Create(&agent).Error; err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}
	return &agent, nil
}

func (s *AgentService) Update(ctx context.Context, id uint, in AgentInput) (*model.Agent, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}

	agent, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	agent.Name = strings.TrimSpace(in.Name)
	agent.Description = in.Description
	agent.Command = strings.TrimSpace(in.Command)
	agent.Args = in.Args
	agent.Env = in.Env
	agent.Cwd = in.Cwd
	if in.Status != "" {
		agent.Status = model.AgentStatus(in.Status)
	}

	if err := s.db.WithContext(ctx).Save(agent).Error; err != nil {
		return nil, fmt.Errorf("update agent %d: %w", id, err)
	}
	return agent, nil
}

// CatalogItem 是配置页对一个条目的取舍；Key 对模型是 id、对命令是 name。
type CatalogItem struct {
	Key      string `json:"key"`
	Disabled bool   `json:"disabled"`
	// Alias 是模型的显示别名（只对 models 有意义），指针区分
	// 「不动」（nil）与「清空回原名」（空串）。
	Alias *string `json:"alias,omitempty"`
}

// CatalogInput 更新探测缓存条目的启用状态；nil 切片表示这一类不动。
type CatalogInput struct {
	Models   []CatalogItem `json:"models"`
	Commands []CatalogItem `json:"commands"`
	// FastPolicy 更新快速模式取舍（"on"/"off"），nil 不动。
	FastPolicy *string `json:"fastPolicy,omitempty"`
}

// UpdateCatalog 应用配置页的勾选：只改 disabled/alias 标记与快速模式
// 取舍，清单本身由探测维护。
func (s *AgentService) UpdateCatalog(ctx context.Context, id uint, in CatalogInput) (*model.Agent, error) {
	agent, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if in.Models != nil {
		items := make(map[string]CatalogItem, len(in.Models))
		for _, item := range in.Models {
			items[item.Key] = item
		}
		for i := range agent.Models {
			item, ok := items[agent.Models[i].ID]
			if !ok {
				continue
			}
			agent.Models[i].Disabled = item.Disabled
			if item.Alias != nil {
				agent.Models[i].Alias = strings.TrimSpace(*item.Alias)
			}
		}
	}
	if in.FastPolicy != nil {
		switch *in.FastPolicy {
		case "on", "off":
			agent.FastPolicy = *in.FastPolicy
		default:
			return nil, fmt.Errorf("%w: fastPolicy must be on or off", ErrInvalid)
		}
	}
	if in.Commands != nil {
		disabled := make(map[string]bool, len(in.Commands))
		for _, item := range in.Commands {
			disabled[item.Key] = item.Disabled
		}
		for i := range agent.Commands {
			if v, ok := disabled[agent.Commands[i].Name]; ok {
				agent.Commands[i].Disabled = v
			}
		}
	}

	if err := s.db.WithContext(ctx).Save(agent).Error; err != nil {
		return nil, fmt.Errorf("update agent catalog %d: %w", id, err)
	}
	return agent, nil
}

func (s *AgentService) Delete(ctx context.Context, id uint) error {
	res := s.db.WithContext(ctx).Delete(&model.Agent{}, id)
	if res.Error != nil {
		return fmt.Errorf("delete agent %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("agent %d: %w", id, ErrNotFound)
	}
	return nil
}
