package database

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/Pi-Teacher/server/internal/platform/config"
	"github.com/Pi-Teacher/server/internal/platform/database/migrations"
)

// gooseVersionTable 是 goose 记录已应用迁移版本的表名.
// 设计文档中的 schema_migrations 语义即由这张表承载: 版本管理完全交给
// goose, 项目不另建平行表, 避免两处状态不一致.
const gooseVersionTable = "goose_db_version"

// Migrate 对当前驱动执行全部待应用的迁移.
//
// 迁移期间持有单实例锁, 防止误启动的第二个进程交叉执行 DDL:
// PostgreSQL 用会话级 advisory lock, MySQL 用锁表加心跳续租,
// SQLite 依赖 WAL 单写锁加 goose 的事务化 DDL 串行化.
func Migrate(ctx context.Context, db *DB) error {
	fsys, err := fs.Sub(migrations.FS, migrations.DialectDir(db.Driver))
	if err != nil {
		return fmt.Errorf("open %s migrations: %w", db.Driver, err)
	}

	dialect, err := gooseDialect(db.Driver)
	if err != nil {
		return err
	}

	opts := []goose.ProviderOption{
		goose.WithTableName(gooseVersionTable),
		// 迁移细节不进业务日志, 失败信息由返回错误统一上报.
		goose.WithLogger(goose.NopLogger()),
	}
	if opt, err := migrationLockOption(db); err != nil {
		return err
	} else if opt != nil {
		opts = append(opts, opt)
	}

	provider, err := goose.NewProvider(dialect, db.SQL(), fsys, opts...)
	if err != nil {
		return fmt.Errorf("init migration provider: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	for _, r := range results {
		if r.Error != nil {
			return fmt.Errorf("migration %d %s: %w", r.Source.Version, r.Source.Path, r.Error)
		}
	}
	return nil
}

// gooseDialect 把驱动名映射为 goose 方言常量.
func gooseDialect(driver string) (goose.Dialect, error) {
	switch driver {
	case config.DriverSQLite:
		return goose.DialectSQLite3, nil
	case config.DriverMySQL:
		return goose.DialectMySQL, nil
	case config.DriverPostgres:
		return goose.DialectPostgres, nil
	default:
		return "", fmt.Errorf("unsupported db driver %q", driver)
	}
}

// migrationLockOption 按驱动选择迁移锁:
//   - PostgreSQL: 连接级 advisory lock, 迁移结束随连接释放;
//   - MySQL: 锁表加心跳续租, 兼容没有 advisory lock 的环境;
//   - SQLite: 不加额外锁, 单文件数据库由 WAL 写锁串行化写入.
func migrationLockOption(db *DB) (goose.ProviderOption, error) {
	switch db.Driver {
	case config.DriverMySQL:
		locker, err := lock.NewMySQLTableLocker()
		if err != nil {
			return nil, err
		}
		return goose.WithLocker(locker), nil
	case config.DriverPostgres:
		locker, err := lock.NewPostgresSessionLocker()
		if err != nil {
			return nil, err
		}
		return goose.WithSessionLocker(locker), nil
	default:
		return nil, nil
	}
}
