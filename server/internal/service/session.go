package service

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"acpp/server/internal/model"
)

// SessionService 负责会话与会话消息的读写。
type SessionService struct {
	db *gorm.DB
}

func NewSessionService(db *gorm.DB) *SessionService {
	return &SessionService{db: db}
}

// SessionView 是会话的对外视图，额外带上 agent 名称与消息数。
type SessionView struct {
	model.Session
	AgentName    string `json:"agentName"`
	MessageCount int64  `json:"messageCount"`
}

// SessionInput 是创建会话的入参。
type SessionInput struct {
	AgentID uint   `json:"agentId"`
	Title   string `json:"title"`
	Cwd     string `json:"cwd"`
}

func (s *SessionService) List(ctx context.Context, agentID uint) ([]SessionView, error) {
	q := s.db.WithContext(ctx).Model(&model.Session{}).Preload("Agent")
	if agentID != 0 {
		q = q.Where("agent_id = ?", agentID)
	}

	var sessions []model.Session
	if err := q.Order("updated_at desc").Find(&sessions).Error; err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	views := make([]SessionView, 0, len(sessions))
	for i := range sessions {
		view, err := s.toView(ctx, &sessions[i])
		if err != nil {
			return nil, err
		}
		views = append(views, *view)
	}
	return views, nil
}

func (s *SessionService) Get(ctx context.Context, id uint) (*SessionView, error) {
	var session model.Session
	err := s.db.WithContext(ctx).Preload("Agent").First(&session, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("session %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get session %d: %w", id, err)
	}
	return s.toView(ctx, &session)
}

func (s *SessionService) Create(ctx context.Context, in SessionInput) (*SessionView, error) {
	if in.AgentID == 0 {
		return nil, fmt.Errorf("%w: agentId is required", ErrInvalid)
	}

	var agent model.Agent
	err := s.db.WithContext(ctx).First(&agent, in.AgentID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("agent %d: %w", in.AgentID, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load agent %d: %w", in.AgentID, err)
	}

	cwd := in.Cwd
	if cwd == "" {
		cwd = agent.Cwd
	}

	session := model.Session{
		AgentID: agent.ID,
		Title:   in.Title,
		Cwd:     cwd,
		State:   model.SessionActive,
	}
	if err := s.db.WithContext(ctx).Create(&session).Error; err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	session.Agent = &agent
	return s.toView(ctx, &session)
}

func (s *SessionService) Delete(ctx context.Context, id uint) error {
	res := s.db.WithContext(ctx).Delete(&model.Session{}, id)
	if res.Error != nil {
		return fmt.Errorf("delete session %d: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("session %d: %w", id, ErrNotFound)
	}
	return nil
}

// Messages 按时间正序返回会话内的全部消息。
func (s *SessionService) Messages(ctx context.Context, sessionID uint) ([]model.Message, error) {
	if _, err := s.Get(ctx, sessionID); err != nil {
		return nil, err
	}

	var messages []model.Message
	err := s.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("id asc").
		Find(&messages).Error
	if err != nil {
		return nil, fmt.Errorf("list messages of session %d: %w", sessionID, err)
	}
	return messages, nil
}

// AppendMessage 往会话追加一条消息，并刷新会话的 updated_at。
func (s *SessionService) AppendMessage(ctx context.Context, sessionID uint, msg model.Message) (*model.Message, error) {
	if _, err := s.Get(ctx, sessionID); err != nil {
		return nil, err
	}

	msg.ID = 0
	msg.SessionID = sessionID
	if msg.Role == "" {
		msg.Role = model.RoleUser
	}
	if msg.Kind == "" {
		msg.Kind = model.KindText
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&msg).Error; err != nil {
			return fmt.Errorf("create message: %w", err)
		}
		return tx.Model(&model.Session{}).
			Where("id = ?", sessionID).
			Update("updated_at", msg.CreatedAt).Error
	})
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

func (s *SessionService) toView(ctx context.Context, session *model.Session) (*SessionView, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&model.Message{}).
		Where("session_id = ?", session.ID).
		Count(&count).Error
	if err != nil {
		return nil, fmt.Errorf("count messages of session %d: %w", session.ID, err)
	}

	view := SessionView{Session: *session, MessageCount: count}
	if session.Agent != nil {
		view.AgentName = session.Agent.Name
	}
	return &view, nil
}
