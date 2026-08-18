package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"acpp/server/internal/model"
)

// ErrForbidden 表示身份不足以执行该操作，由 HTTP 层翻译成 403。
var ErrForbidden = errors.New("forbidden")

// ErrUnauthorized 表示请求没有有效身份，由 HTTP 层翻译成 401。
var ErrUnauthorized = errors.New("unauthorized")

// tenantNameRe 限制租户名：它同时是 root 的目录名，所以只放行文件系统
// 与 URL 都安全的字符，且必须以字母数字开头（挡住隐藏目录）。
var tenantNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,31}$`)

// lastSeenThrottle 是 LastSeenAt 的写库节流窗口。每个请求都 UPDATE 一次
// 会让 SSE 这种长轮询场景把库写爆，分钟级精度对「谁还在用」足够。
const lastSeenThrottle = time.Minute

// TenantService 负责租户的增删改查与 root 目录准备（adr-007）。
type TenantService struct {
	db *gorm.DB
	// base 是全部租户 root 的父目录（默认 ~/acpp），租户 root = base/<name>。
	base string
}

// NewTenantService 用 base 作为租户 root 的父目录；base 为空时取默认工作区。
func NewTenantService(db *gorm.DB, base string) *TenantService {
	if base == "" {
		base = DefaultCwd()
	}
	return &TenantService{db: db, base: base}
}

// TenantView 是给 owner 的租户视图，比模型多一个邀请 token——只有 owner
// 能看到它，因为拿到 token 就等于拿到这个租户的全部会话。
type TenantView struct {
	model.Tenant
	// InviteToken 供 owner 拼邀请链接（前端拼 `<origin>/?invite=<token>`）。
	InviteToken string `json:"inviteToken"`
	// SessionCount 是该租户名下的会话数，让 owner 停用前心里有数。
	SessionCount int64 `json:"sessionCount"`
}

// List 分页返回访客列表（每条带会话数）。
func (s *TenantService) List(ctx context.Context, page, pageSize int) ([]TenantView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	q := s.db.WithContext(ctx).Model(&model.Tenant{})
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count tenants: %w", err)
	}

	var tenants []model.Tenant
	err := q.Order("id asc").
		Limit(pageSize).Offset((page - 1) * pageSize).
		Find(&tenants).Error
	if err != nil {
		return nil, 0, fmt.Errorf("list tenants: %w", err)
	}

	views := make([]TenantView, 0, len(tenants))
	for i := range tenants {
		var count int64
		if err := s.db.WithContext(ctx).Model(&model.Session{}).
			Where("tenant_id = ?", tenants[i].ID).Count(&count).Error; err != nil {
			return nil, 0, fmt.Errorf("count tenant sessions: %w", err)
		}
		views = append(views, TenantView{
			Tenant:       tenants[i],
			InviteToken:  tenants[i].Token,
			SessionCount: count,
		})
	}
	return views, total, nil
}

func (s *TenantService) Get(ctx context.Context, id uint) (*model.Tenant, error) {
	var tenant model.Tenant
	err := s.db.WithContext(ctx).First(&tenant, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: tenant %d", ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant: %w", err)
	}
	return &tenant, nil
}

// TenantInput 是建租户的入参。
type TenantInput struct {
	Name string `json:"name"`
}

// Create 建租户：生成凭证、落 root 目录。root 目录立刻创建而不是等首次
// 使用——目录选择器一进来就得有个能站住的位置。
func (s *TenantService) Create(ctx context.Context, in TenantInput) (*TenantView, error) {
	name := strings.TrimSpace(in.Name)
	if !tenantNameRe.MatchString(name) {
		return nil, fmt.Errorf("%w: name must be 1-32 chars of letters, digits, . _ -", ErrInvalid)
	}
	token, err := newToken()
	if err != nil {
		return nil, err
	}

	root := filepath.Join(s.base, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create tenant root: %w", err)
	}

	tenant := model.Tenant{Name: name, Token: token, Root: root}
	if err := s.db.WithContext(ctx).Create(&tenant).Error; err != nil {
		return nil, fmt.Errorf("%w: tenant name already taken", ErrInvalid)
	}
	return &TenantView{Tenant: tenant, InviteToken: tenant.Token}, nil
}

// Rotate 重新生成分享链接：旧链接立刻作废（谁拿着旧 URL 都进不来），
// 会话与目录不动。转发错人、链接外泄时用这个而不是删租户。
func (s *TenantService) Rotate(ctx context.Context, id uint) (*TenantView, error) {
	tenant, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	token, err := newToken()
	if err != nil {
		return nil, err
	}

	if err := s.db.WithContext(ctx).Model(tenant).Update("token", token).Error; err != nil {
		return nil, fmt.Errorf("rotate tenant token: %w", err)
	}
	tenant.Token = token
	return &TenantView{Tenant: *tenant, InviteToken: token}, nil
}

// TenantPatch 是租户的可改项。Name 不在其中：它是 root 的目录名，改名
// 会让已建会话的 cwd 悬空。
type TenantPatch struct {
	Disabled *bool   `json:"disabled"`
	Root     *string `json:"root"`
}

func (s *TenantService) Update(ctx context.Context, id uint, patch TenantPatch) (*model.Tenant, error) {
	tenant, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	updates := map[string]any{}
	if patch.Disabled != nil {
		updates["disabled"] = *patch.Disabled
	}
	if patch.Root != nil {
		root := strings.TrimSpace(*patch.Root)
		if !filepath.IsAbs(root) {
			return nil, fmt.Errorf("%w: root must be an absolute path", ErrInvalid)
		}
		root = filepath.Clean(root)
		if err := os.MkdirAll(root, 0o755); err != nil {
			return nil, fmt.Errorf("create tenant root: %w", err)
		}
		updates["root"] = root
	}
	if len(updates) == 0 {
		return tenant, nil
	}

	if err := s.db.WithContext(ctx).Model(tenant).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("update tenant: %w", err)
	}
	return s.Get(ctx, id)
}

// Delete 只删记录与凭证，不动磁盘目录，也不删会话——删人不该顺手销毁
// 他干过的活。遗留会话归 owner 可见（租户名显示为已删除）。
func (s *TenantService) Delete(ctx context.Context, id uint) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Delete(&model.Tenant{}, id).Error; err != nil {
		return fmt.Errorf("delete tenant: %w", err)
	}
	return nil
}

// Authenticate 用凭证换租户身份，顺带节流刷新 LastSeenAt。
//
// 两种失败刻意分开：凭证不认识是 ErrUnauthorized（「你需要一个邀请链接」），
// 凭证认识但被停用是 ErrForbidden（「你的访问已被关闭」）。界面要给出的
// 是完全不同的两句话，混成一种错误就分不出来了。停用每次鉴权现查，
// 即时生效，不用等 cookie 过期。
func (s *TenantService) Authenticate(ctx context.Context, token string) (*model.Tenant, error) {
	if strings.TrimSpace(token) == "" {
		return nil, ErrUnauthorized
	}

	var tenant model.Tenant
	err := s.db.WithContext(ctx).Where("token = ?", token).First(&tenant).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, fmt.Errorf("authenticate tenant: %w", err)
	}
	if tenant.Disabled {
		return &tenant, fmt.Errorf("%w: access disabled", ErrForbidden)
	}

	now := time.Now()
	if tenant.LastSeenAt == nil || now.Sub(*tenant.LastSeenAt) > lastSeenThrottle {
		if err := s.db.WithContext(ctx).Model(&tenant).
			UpdateColumn("last_seen_at", now).Error; err != nil {
			// 活跃时间只是展示信息，写失败不该拦住请求。
			return &tenant, nil
		}
		tenant.LastSeenAt = &now
	}
	return &tenant, nil
}

func newToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate tenant token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
