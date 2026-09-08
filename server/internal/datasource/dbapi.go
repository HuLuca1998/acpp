package datasource

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// 租户与脚本的只读 HTTP 面（/api/db，adr-021）的业务逻辑。
//
// 会话侧那几条按工作目录推项目——那是给「开在某个项目里的会话」设计的
// 隔离底座。脚本没有「所在项目」这回事，硬要它拼一个目录出来只会拼错
//（实测：拼项目名对不上、拼租户根下的路径也对不上），所以这一面改按
// 数据源标识寻址：列清单拿 ref，拿 ref 发查询。
//
// 永远只读：Execute 的 allowWrite 固定为 false，与 AI 的 db_query 同一条
// 护栏。要改数据走界面或 AI 的执行工具——脚本面一旦能写，租户 token 就
// 等于一把生产库的写钥匙，而它的分发（邀请链接）远没有那么郑重。

// Enabled 列出全部启用的数据源；project 非空时只要那个项目的（不区分
// 大小写）。不分页：数据源是个位到十位的量级。空结果回空切片不回 nil，
// 省得 HTTP 层再判一次。
func (s *Service) Enabled(ctx context.Context, project string) ([]model.DataSource, error) {
	q := s.db.WithContext(ctx).Where("disabled = ?", false)
	if p := strings.TrimSpace(project); p != "" {
		q = q.Where("LOWER(project) = ?", strings.ToLower(p))
	}
	out := []model.DataSource{}
	if err := q.Order("project, env").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list enabled datasources: %w", err)
	}
	for i := range out {
		s.finish(ctx, &out[i])
	}
	return out, nil
}

// RefQueryResult 是 ReadQuery 的结果：比 ExecResult 多一个 Source——调用方
// 可能只给了环境名，回给它实际落到了哪条数据源。
type RefQueryResult struct {
	Source string `json:"source"`
	*ExecResult
}

// ReadQuery 按数据源标识执行只读查询。ref 可以是 `<项目>/<环境>`、环境名
// （全局唯一时）或数据源 id；只有一条启用数据源时可省略。
func (s *Service) ReadQuery(ctx context.Context, ref, script string, maxRows int) (*RefQueryResult, error) {
	sources, err := s.Enabled(ctx, "")
	if err != nil {
		return nil, err
	}
	picked, err := resolveOrID(sources, ref)
	if err != nil {
		return nil, err
	}
	// 清单里的记录不带密码，连接要用完整记录。
	src, err := s.Get(ctx, picked.ID)
	if err != nil {
		return nil, err
	}
	res, err := Execute(ctx, src, "", script, maxRows, false)
	if err != nil {
		return nil, err
	}
	return &RefQueryResult{Source: src.Ref, ExecResult: res}, nil
}

// resolveOrID 在 Resolve 之上多认一种写法：纯数字按 id——清单接口回的
// 就有 id，脚本照抄最省事。
func resolveOrID(sources []model.DataSource, ref string) (*model.DataSource, error) {
	ref = strings.TrimSpace(ref)
	if id, err := strconv.ParseUint(ref, 10, 64); err == nil {
		for i := range sources {
			if sources[i].ID == uint(id) {
				return &sources[i], nil
			}
		}
		return nil, fmt.Errorf("%w: 没有 id 为 %d 的启用数据源", service.ErrNotFound, id)
	}
	return Resolve(sources, ref)
}
