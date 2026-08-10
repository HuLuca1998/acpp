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
