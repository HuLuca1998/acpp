package orch

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// RoleService 负责编排角色的读写与内置模板预置（adr-006）。
type RoleService struct {
	db *gorm.DB
}

func NewRoleService(db *gorm.DB) *RoleService {
	return &RoleService{db: db}
}

// RoleInput 是创建/更新角色的入参。
type RoleInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Persona     string `json:"persona"`
	AgentID     uint   `json:"agentId"`
	Model       string `json:"model"`
	Effort      string `json:"effort"`
	Level       string `json:"level"`
}

func (in RoleInput) validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("%w: name is required", service.ErrInvalid)
	}
	if in.AgentID == 0 {
		return fmt.Errorf("%w: agentId is required", service.ErrInvalid)
	}
	return nil
}

// List 返回全部角色。编排的调度提示词要把可雇佣角色一个不落地列出来
// （buildOrchPrompt），所以这条路径刻意不分页——分页的是给人看的列表。
func (s *RoleService) List(ctx context.Context) ([]model.Role, error) {
	var roles []model.Role
	if err := s.db.WithContext(ctx).Order("id asc").Find(&roles).Error; err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	return roles, nil
}

// ListPage 是角色页用的分页读法。
func (s *RoleService) ListPage(ctx context.Context, page, pageSize int) ([]model.Role, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}

	q := s.db.WithContext(ctx).Model(&model.Role{})
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count roles: %w", err)
	}

	var roles []model.Role
	err := q.Order("id asc").
		Limit(pageSize).Offset((page - 1) * pageSize).
		Find(&roles).Error
	if err != nil {
		return nil, 0, fmt.Errorf("list roles: %w", err)
	}
	return roles, total, nil
}

func (s *RoleService) Get(ctx context.Context, id uint) (*model.Role, error) {
	var role model.Role
	err := s.db.WithContext(ctx).First(&role, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("role %d: %w", id, service.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get role %d: %w", id, err)
	}
	return &role, nil
}

// GetByName 按名字取角色——spawn_agent 工具的入参是角色名（AI 记名字
// 比记 id 可靠）。
func (s *RoleService) GetByName(ctx context.Context, name string) (*model.Role, error) {
	var role model.Role
	err := s.db.WithContext(ctx).Where("name = ?", strings.TrimSpace(name)).First(&role).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("role %q: %w", name, service.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get role %q: %w", name, err)
	}
	return &role, nil
}

func (s *RoleService) Create(ctx context.Context, in RoleInput) (*model.Role, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	role := model.Role{
		Name:        strings.TrimSpace(in.Name),
		Description: in.Description,
		Persona:     in.Persona,
		AgentID:     in.AgentID,
		Model:       in.Model,
		Effort:      in.Effort,
		Level:       in.Level,
	}
	if err := s.db.WithContext(ctx).Create(&role).Error; err != nil {
		return nil, fmt.Errorf("create role: %w", err)
	}
	return &role, nil
}

func (s *RoleService) Update(ctx context.Context, id uint, in RoleInput) (*model.Role, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	role, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	role.Name = strings.TrimSpace(in.Name)
	role.Description = in.Description
	role.Persona = in.Persona
	role.AgentID = in.AgentID
	role.Model = in.Model
	role.Effort = in.Effort
	role.Level = in.Level
	if err := s.db.WithContext(ctx).Save(role).Error; err != nil {
		return nil, fmt.Errorf("update role %d: %w", id, err)
	}
	return role, nil
}

func (s *RoleService) Delete(ctx context.Context, id uint) error {
	res := s.db.WithContext(ctx).Delete(&model.Role{}, id)
	if res.Error != nil {
		return fmt.Errorf("delete role %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("role %d: %w", id, service.ErrNotFound)
	}
	return nil
}

// builtinRole 是内置模板的静态定义；AgentID 在预置时按 agent name 解析。
type builtinRole struct {
	name        string
	agentName   string
	description string
	persona     string
	level       string
}

// builtinRoles 是首启预置的四个流水线角色。权限档默认完全放行
// （用户拍板：编排追求无人值守，权限约束靠 persona 纪律而非弹卡；
// 需要收紧时在角色页改）。
// persona 写给子会话看，description 写给主会话的雇佣目录看。
var builtinRoles = []builtinRole{
	{
		name:        "分析员",
		agentName:   "claude",
		description: "调研与分析：阅读代码/文档、定位问题根因、梳理现状并产出结论。只读，不改任何文件。",
		persona: "你是团队的分析员。你的职责是调研与分析：阅读代码与文档、定位问题、梳理现状，" +
			"产出清晰的结论与建议。铁律：你只读不写——不创建、不修改、不删除任何文件，" +
			"不执行有副作用的命令。最终回复要给出结构化的分析结论，让下游角色能直接开工。",
		level: "full",
	},
	{
		name:        "开发者",
		agentName:   "claude",
		description: "编码实现：按任务说明修改代码、实现功能、修复缺陷。团队里唯一有写权限的角色。",
		persona: "你是团队的开发者，团队里唯一被授权修改文件的角色。按任务说明实现改动，" +
			"改完自行验证（编译/测试）。最终回复要说清改了哪些文件、为什么、验证结果如何。",
		level: "full",
	},
	{
		name:        "审查者",
		agentName:   "codex",
		description: "代码审查：检查指定改动的正确性、风格与风险，产出审查意见。只读，不改任何文件。",
		persona: "你是团队的审查者。审查指定的代码改动：正确性、边界条件、风格一致性、潜在风险。" +
			"铁律：你只读不写——发现问题描述清楚位置与理由即可，修改由开发者执行。" +
			"最终回复按严重程度列出问题清单，没有问题就明说通过。",
		level: "full",
	},
	{
		name:        "测试员",
		agentName:   "claude",
		description: "运行测试与验证：执行测试命令、复现问题、报告结果。可执行命令但不修改源码。",
		persona: "你是团队的测试员。职责是运行测试、复现问题、验证改动效果。" +
			"你可以执行测试与构建命令，但不修改任何源码文件。" +
			"最终回复要包含执行的命令、完整的结果摘要、失败用例的具体信息。",
		level: "full",
	},
}

// EnsureDefaults 为缺失的内置角色补建记录（按 name 判存，不覆盖用户
// 修改；用户删除的不复活——与 Agent 预置不同，角色允许被永久删除，
// 因此这里只在「角色表从未有过内置角色」时整批预置一次）。
func (s *RoleService) EnsureDefaults(ctx context.Context) error {
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.Role{}).
		Where("builtin = ?", true).Count(&count).Error; err != nil {
		return fmt.Errorf("check builtin roles: %w", err)
	}
	if count > 0 {
		return nil
	}

	var seeded int64
	if err := s.db.WithContext(ctx).Model(&model.Role{}).Count(&seeded).Error; err != nil {
		return fmt.Errorf("count roles: %w", err)
	}
	if seeded > 0 {
		// 用户自建过角色且没有内置标记（老库或全删过内置），不强塞。
		return nil
	}

	for _, b := range builtinRoles {
		var agent model.Agent
		err := s.db.WithContext(ctx).Where("name = ?", b.agentName).First(&agent).Error
		if err != nil {
			// 内置工具缺失（异常库），跳过该角色而不是让启动失败。
			continue
		}
		role := model.Role{
			Name:        b.name,
			Description: b.description,
			Persona:     b.persona,
			AgentID:     agent.ID,
			Level:       b.level,
			Builtin:     true,
		}
		if err := s.db.WithContext(ctx).Create(&role).Error; err != nil {
			return fmt.Errorf("seed role %s: %w", b.name, err)
		}
	}
	return nil
}
