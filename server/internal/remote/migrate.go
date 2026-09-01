package remote

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"acpp/server/internal/model"
	"acpp/server/internal/sshdial"
)

// MigrateFromDataSources 把数据源里的 SSH 跳板配置搬进服务器表（adr-019）。
//
// 启动时跑一次，**幂等**：只认「开着隧道且还没关联服务器」的记录，跑过第二遍
// 什么都不做。同一台机器（host + port + user 相同）被多条数据源当跳板时只建
// 一条，那正是这次改动要消掉的重复。
//
// 迁移失败不该挡住服务启动：搬不动的那条数据源在下次查询时会明确报
// 「没有关联跳板机」，比进程起不来好定位得多。所以返回的错误由调用方
// 记日志而不是中断。
func (s *Service) MigrateFromDataSources(ctx context.Context) error {
	var pending []model.DataSource
	err := s.db.WithContext(ctx).
		Where("ssh_enabled = ? AND (server_id = 0 OR server_id IS NULL)", true).
		Find(&pending).Error
	if err != nil {
		return fmt.Errorf("扫描待迁移数据源: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	// 同一次迁移里建过的机器记在这，避免三条数据源共用一台跳板时建出三条。
	made := map[string]uint{}
	var migrated, skipped int
	for i := range pending {
		src := &pending[i]
		host := strings.TrimSpace(src.SSHHost)
		if host == "" {
			// 开着隧道却没填跳板机地址：这条本来就连不上，跳过并留痕。
			slog.Warn("数据源开着 SSH 但没有跳板机地址，跳过迁移",
				"datasource", src.ID, "ref", src.Project+"/"+src.Env)
			skipped++
			continue
		}
		port := src.SSHPort
		if port <= 0 {
			port = 22
		}
		user := strings.TrimSpace(src.SSHUser)
		key := host + ":" + strconv.Itoa(port) + ":" + user

		id, ok := made[key]
		if !ok {
			srv, err := s.findOrCreate(ctx, src, host, port, user)
			if err != nil {
				slog.Error("迁移数据源的 SSH 配置失败",
					"datasource", src.ID, "ref", src.Project+"/"+src.Env, "err", err)
				skipped++
				continue
			}
			id = srv.ID
			made[key] = id
		}

		if err := s.db.WithContext(ctx).Model(src).Update("server_id", id).Error; err != nil {
			slog.Error("回填数据源的服务器关联失败", "datasource", src.ID, "server", id, "err", err)
			skipped++
			continue
		}
		migrated++
	}

	slog.Info("SSH 跳板配置迁移完成（adr-019）",
		"migrated", migrated, "skipped", skipped, "servers", len(made))
	return nil
}

// findOrCreate 找一台地址与账号都对得上的机器，没有就按数据源的旧配置建一台。
func (s *Service) findOrCreate(ctx context.Context, src *model.DataSource,
	host string, port int, user string) (*model.Server, error) {
	var existing model.Server
	err := s.db.WithContext(ctx).
		Where("host = ? AND port = ? AND user = ?", host, port, user).
		First(&existing).Error
	if err == nil {
		return &existing, nil
	}

	auth := strings.TrimSpace(src.SSHAuth)
	if !sshdial.ValidAuth(auth) {
		auth = sshdial.AuthPassword
	}
	srv := model.Server{
		Name:       s.uniqueName(ctx, host),
		Host:       host,
		Port:       port,
		User:       user,
		Auth:       auth,
		Password:   src.SSHPassword,
		KeyPath:    src.SSHKeyPath,
		Passphrase: src.SSHPassphrase,
		Note:       "自 数据源 " + src.Project + "/" + src.Env + " 的 SSH 配置迁移（adr-019）",
	}
	if err := s.db.WithContext(ctx).Create(&srv).Error; err != nil {
		return nil, fmt.Errorf("建服务器记录: %w", err)
	}
	return &srv, nil
}

// uniqueName 用主机名当名字，重名就往后加 -2、-3。
func (s *Service) uniqueName(ctx context.Context, host string) string {
	base := host
	for n := 1; n < 100; n++ {
		name := base
		if n > 1 {
			name = base + "-" + strconv.Itoa(n)
		}
		var count int64
		err := s.db.WithContext(ctx).Model(&model.Server{}).
			Where("name = ?", name).Count(&count).Error
		if err == nil && count == 0 {
			return name
		}
	}
	return base + "-" + strconv.FormatInt(int64(len(base)), 36)
}
