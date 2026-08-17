package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"
)

// Scope 是一次请求的可见范围与路径边界（adr-007）。
//
// 它是隔离的**唯一执行点**：数据面靠 FilterSessions 把租户条件写进查询本身
// （漏写等于查不到，不会变成越权），路径面靠 GuardPath / GuardNewPath 把
// 一切目录操作钉在租户 root 内。owner 的 Scope 恒真，两个方法都放行。
type Scope struct {
	// Owner 为 true 时不过滤数据、不限制路径。
	Owner bool
	// TenantID 是会话归属列的值；owner 的会话记 0。
	TenantID uint
	// Root 是租户的最上层工作目录；owner 为空。
	Root string
}

// OwnerScope 是本机 owner 的全权范围。
func OwnerScope() Scope { return Scope{Owner: true} }

// TenantScope 是某个租户的受限范围。
func TenantScope(tenantID uint, root string) Scope {
	return Scope{TenantID: tenantID, Root: root}
}

// Home 是该身份的起始目录：owner 用家目录（历史行为不变），租户用自己的
// root——目录选择器一进来就该站在自己的地盘上。
func Home(s Scope) string {
	if s.Owner {
		home, err := os.UserHomeDir()
		if err != nil {
			return DefaultCwd()
		}
		return home
	}
	return s.Root
}

// GuardPath 把一个**已存在**的路径解析成 canonical 形式并校验它落在 root 内。
// 符号链接先解析再比对，防止用链接把 root 指到外面。
func (s Scope) GuardPath(path string) (string, error) {
	if s.Owner {
		if path == "" {
			return Home(s), nil
		}
		if !filepath.IsAbs(path) {
			return "", fmt.Errorf("%w: path must be absolute", ErrInvalid)
		}
		return filepath.Clean(path), nil
	}

	canonRoot, err := s.canonRoot()
	if err != nil {
		return "", err
	}
	if path == "" {
		return canonRoot, nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(canonRoot, path)
	}
	canon, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	if !within(canonRoot, canon) {
		return "", fmt.Errorf("%w: path escapes workspace root", ErrForbidden)
	}
	return canon, nil
}

// GuardNewPath 校验一个**待创建**的路径（新建目录、克隆目标）：自身可以
// 还不存在，但父目录必须已存在且在 root 内——否则 EvalSymlinks 会因为
// 目标不存在直接失败，把正常的新建也一起挡掉。
func (s Scope) GuardNewPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%w: path is required", ErrInvalid)
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: path must be absolute", ErrInvalid)
	}
	clean := filepath.Clean(path)
	if s.Owner {
		return clean, nil
	}

	parent, err := s.GuardPath(filepath.Dir(clean))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(clean)), nil
}

// FilterSessions 给会话查询加上归属条件。owner 不加，看全部。
func (s Scope) FilterSessions(q *gorm.DB) *gorm.DB {
	if s.Owner {
		return q
	}
	return q.Where("tenant_id = ?", s.TenantID)
}

// canonRoot 解析租户 root 的 canonical 形式。root 是建租户时创建的，
// 拿不到说明它被人为删了——这时一切路径操作都该失败而不是放行。
func (s Scope) canonRoot() (string, error) {
	if s.Root == "" {
		return "", fmt.Errorf("%w: tenant has no workspace root", ErrForbidden)
	}
	canon, err := filepath.EvalSymlinks(s.Root)
	if err != nil {
		// root 被删了就补回来：租户不该因为目录消失而彻底不能用。
		if os.IsNotExist(err) {
			if mkErr := os.MkdirAll(s.Root, 0o755); mkErr == nil {
				return filepath.EvalSymlinks(s.Root)
			}
		}
		return "", fmt.Errorf("%w: workspace root unavailable: %s", ErrInvalid, err)
	}
	return canon, nil
}

// within 判断 path 是否等于 root 或在 root 之下。
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
