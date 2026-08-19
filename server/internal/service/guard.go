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

// Home 是该身份的起始目录：owner 用设置里的工作区根，租户用自己的 root。
//
// owner 原先从家目录起步——一进目录选择器就站在整台机器的根上，找项目
// 得翻好几层。工作区根是「干活的地方」，作为起点更合手；owner 仍可以
// 往上翻到任意目录（路径闸对他恒真）。
func Home(s Scope) string {
	if s.Owner {
		root := DefaultCwd()
		if err := os.MkdirAll(root, 0o755); err != nil {
			home, homeErr := os.UserHomeDir()
			if homeErr != nil {
				return root
			}
			return home
		}
		return root
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
		// `~` 展开只给 owner：表单里私钥路径、文件选择器起点这类地方
		// 都按 CLI 直觉写 ~/.ssh；租户分支的相对路径 join 在自己 root
		// 下，`~` 对他们不该有家目录语义。
		if path == "~" || strings.HasPrefix(path, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("%w: resolve home: %v", ErrInvalid, err)
			}
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
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

	// 已经存在的路径按已存在校验——否则连租户 root 自身都会被拒（它的父
	// 目录是所有租户的公共 base，当然不在自己的 root 内）。
	if _, err := os.Stat(clean); err == nil {
		return s.GuardPath(clean)
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
