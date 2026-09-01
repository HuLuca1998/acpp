package remote

import (
	"context"
	"fmt"
	"strings"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
)

// @ 引用一台服务器：把「这轮说的是这台机器」告诉模型。
//
// 引用只给**机器**，不带路径（adr-019）。要看哪个目录由模型从项目代码
// 推断——那是这套东西的主线：代码是事实源，工具只负责验证推断。带上路径
// 反而会让它省掉读代码那一步，路径过时了也无从发现。

// Reference 展开一组服务器引用。不收 cwd：服务器不做项目隔离。
func (s *Service) Reference(ctx context.Context, refs []string) ([]service.ServerReference, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	list, err := s.Enabled(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]service.ServerReference, 0, len(refs))
	for _, raw := range refs {
		srv, err := resolveByName(list, raw)
		if err != nil {
			return nil, err
		}
		out = append(out, expandServerRef(srv))
	}
	return out, nil
}

func resolveByName(list []model.Server, raw string) (*model.Server, error) {
	name := strings.TrimSpace(raw)
	for i := range list {
		if strings.EqualFold(list[i].Name, name) {
			return &list[i], nil
		}
	}
	return nil, fmt.Errorf("%w: 没有叫 %q 的服务器", service.ErrNotFound, raw)
}

// expandServerRef 拼那段告知。
//
// 语气与数据库引用同源：表达的是「接下来的问题针对这台机器，你去看它」，
// 不是「这是一份状态快照」。不点破的话，模型容易凭印象描述线上情况。
func expandServerRef(srv *model.Server) service.ServerReference {
	var b strings.Builder
	b.WriteString("# 本轮指定的服务器\n\n")
	fmt.Fprintf(&b, "用户引用了服务器 **%s**（%s@%s:%d", srv.Name, srv.User, srv.Host, srv.Port)
	if note := strings.TrimSpace(srv.Note); note != "" {
		fmt.Fprintf(&b, "，%s", note)
	}
	b.WriteString("）。\n\n")
	fmt.Fprintf(&b, "接下来凡是与线上状态有关的问题，都用 acpp-server 的工具实际去看它"+
		"（`server` 参数填 `%s`）：容器状态看 docker_ps，日志看 docker_logs 或 server_read/server_grep，"+
		"资源看 server_info。**远程路径先从项目代码推断**（compose 里的挂载与容器名、部署脚本里的目录），"+
		"再用工具验证，不要凭印象猜路径，也不要凭印象描述线上情况。"+
		"这些工具全部只读——要改线上，把要做的事和理由说清楚交给用户。\n", srv.Name)

	return service.ServerReference{URI: service.ServerRefScheme + srv.Name, Text: b.String()}
}
