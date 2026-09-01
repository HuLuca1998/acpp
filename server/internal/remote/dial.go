package remote

import (
	"context"

	"golang.org/x/crypto/ssh"

	"acpp/server/internal/model"
	"acpp/server/internal/sshdial"
)

// sshdialDial 是拨号的唯一入口：把服务器记录翻成拨号参数，并把 sshdial 的
// 配置类错误换成本项目的哨兵错误。抽成函数是为了让 exec 与 Test 走同一条路。
func sshdialDial(ctx context.Context, srv *model.Server) (*ssh.Client, error) {
	client, err := sshdial.Dial(ctx, configOf(srv))
	if err != nil {
		return nil, wrapDial(err)
	}
	return client, nil
}

// asExitError 把错误链里的 ssh.ExitError 取出来。命令以非 0 退出是**信息**
// 不是故障（「没有那个文件」正是模型要读到的东西），必须与连接失败区分开。
func asExitError(err error, target **ssh.ExitError) bool {
	for err != nil {
		if e, ok := err.(*ssh.ExitError); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
