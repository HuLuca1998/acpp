package db

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"acpp/server/internal/config"
	"acpp/server/internal/model"
)

// Open 打开 sqlite 并执行迁移。DSN 所在目录会被自动创建。
func Open(cfg config.Config) (*gorm.DB, error) {
	if dir := filepath.Dir(cfg.DSN); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
	}

	level := logger.Warn
	if cfg.Debug {
		level = logger.Info
	}

	gdb, err := gorm.Open(sqlite.Open(cfg.DSN), &gorm.Config{
		Logger: logger.Default.LogMode(level),
		// 关掉默认事务：GORM 会把每一次 Create/Update 单独包一层事务，
		// 而 SQLite 的事务提交要落盘。会话元数据的写入（用量快照、设置
		// 快照、状态流转）在一轮里能来几十次，白白多几十次 fsync。
		// 本项目没有跨表原子写的需求，真需要的地方显式 Transaction。
		SkipDefaultTransaction: true,
		// 缓存准备语句：会话列表、归属校验这类查询每秒要跑很多遍，
		// 复用已编译的语句省掉每次的 SQL 解析与计划生成。
		PrepareStmt: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", cfg.DSN, err)
	}

	if err := tune(gdb); err != nil {
		return nil, err
	}

	if err := Migrate(gdb); err != nil {
		return nil, err
	}

	return gdb, nil
}

// Migrate 建表并补齐字段。
func Migrate(gdb *gorm.DB) error {
	// messages 表已退役（adr-003）、roles/orch_sessions/orch_tasks 随编排下线
	// 退役（adr-012）：旧库里已存在的表不动，这里不再创建。
	if err := gdb.AutoMigrate(&model.Agent{}, &model.Session{}, &model.SkillUsage{},
		&model.Tenant{}, &model.DataSource{}, &model.MCPCall{}, &model.Server{}, &model.APILog{}); err != nil {
		return fmt.Errorf("automigrate: %w", err)
	}
	return nil
}

// tune 设定 SQLite 的运行参数与连接池。
//
// 默认的 journal_mode=DELETE 是这套界面卡顿的隐形来源：写一次就要独占整个
// 库文件，而本服务在一轮对话里持续写会话元数据（用量、设置、状态、摘要），
// 同时前端十几个面板还在读。读写互斥的结果就是面板请求随机排队几十毫秒。
// WAL 让读写并行，读永远不被写挡住。
func tune(gdb *gorm.DB) error {
	// synchronous=NORMAL 在 WAL 下是官方推荐档：断电最多丢最近几个事务，
	// 而这是本机的会话元数据（对话本身在转录 JSONL 里），丢了下次重建即可，
	// 换来的是每次提交不再等 fsync。
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		// 写锁被别人占着时最多等 5 秒再报 database is locked，而不是当场
		// 失败——本地并发很浅，等一下必然等得到。
		"PRAGMA busy_timeout = 5000",
		// 负数是 KB 口径：64MB 页缓存，热表整个常驻内存。
		"PRAGMA cache_size = -65536",
		// 临时表与排序走内存，不落磁盘。
		"PRAGMA temp_store = MEMORY",
		// sqlite 默认关闭外键约束，级联删除需要显式打开。
		"PRAGMA foreign_keys = ON",
	}
	for _, p := range pragmas {
		if err := gdb.Exec(p).Error; err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		return fmt.Errorf("sql db handle: %w", err)
	}
	// SQLite 同时只允许一个写者。连接数放开只会让并发写在驱动层排队后
	// 撞上 SQLITE_BUSY；限成小池 + busy_timeout，等待发生在连接层，安静
	// 且可预期。WAL 下读不占写锁，4 条连接对本机面板群绰绰有余。
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(4)
	// 本地文件库的连接没有过期概念，留着复用省掉反复打开的开销。
	sqlDB.SetConnMaxLifetime(0)
	return nil
}
