// Package database 负责打开三种受支持数据库的 GORM 连接.
//
// 项目不在数据库层创建外键和触发器, 所有引用完整性由 Go 服务层在事务中
// 维护, 因此这里也不做任何级联配置.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	gosqlite "gosqlite.org"
	sqlitegorm "gosqlite.org/gorm"

	"github.com/Pi-Teacher/server/internal/platform/config"
)

// SQLite 连接池按 WAL 单写者模型配置: WAL 允许读写并发但写者唯一,
// 连接数放大只会增加写锁竞争, 因此限制为小常数.
const (
	sqliteMaxOpenConns    = 4
	sqliteMaxIdleConns    = 4
	sqliteConnMaxLifetime = time.Hour
	sqliteBusyTimeout     = 10 * time.Second
)

// DB 在 *gorm.DB 之上携带驱动名和底层连接池.
// 迁移器需要直接使用 database/sql 句柄执行 DDL, 因此一并暴露.
type DB struct {
	*gorm.DB

	Driver string
	sqlDB  *sql.DB
}

// SQL 返回底层 database/sql 句柄, 供迁移器使用.
func (d *DB) SQL() *sql.DB { return d.sqlDB }

// Close 释放连接池.
func (d *DB) Close() error {
	if d.sqlDB == nil {
		return nil
	}
	return d.sqlDB.Close()
}

// Open 按配置连接数据库并完成各驱动的初始化校验.
func Open(ctx context.Context, cfg *config.Config) (*DB, error) {
	switch cfg.DBDriver {
	case config.DriverSQLite:
		return openSQLite(cfg.DBDSN)
	case config.DriverMySQL:
		return openViaGorm(ctx, config.DriverMySQL, mysql.Open(cfg.DBDSN))
	case config.DriverPostgres:
		return openViaGorm(ctx, config.DriverPostgres, postgres.Open(cfg.DBDSN))
	default:
		return nil, fmt.Errorf("unsupported db driver %q", cfg.DBDriver)
	}
}

func openSQLite(path string) (*DB, error) {
	// file: 前缀是 URI 形式 DSN (可能带查询参数), :memory: 没有目录概念,
	// 其余情况自动补齐父目录, 避免嵌套路径部署时因缺目录报出难定位的错误.
	if !strings.HasPrefix(path, "file:") && path != ":memory:" {
		if dir := filepath.Dir(path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create sqlite directory %q: %w", dir, err)
			}
		}
	}

	// PRAGMA 逐连接生效, 必须放进 DSN 而不是只对首个连接执行:
	//   - WAL: 读写并发, 崩溃后自动恢复;
	//   - busy_timeout: 写锁竞争时等待而不是立刻报 SQLITE_BUSY;
	//   - synchronous=NORMAL: WAL 下的常规耐久度取舍;
	//   - foreign_keys 关闭: 项目统一使用 Go 逻辑外键.
	pragmas := gosqlite.RecommendedPragmas()
	pragmas.JournalMode = gosqlite.JournalWAL
	pragmas.BusyTimeout = sqliteBusyTimeout
	pragmas.ForeignKeys = false
	pragmas.Synchronous = gosqlite.SynchronousNormal

	gormDB, err := sqlitegorm.OpenConfig(gosqlite.Config{
		Path:            path,
		Pragmas:         pragmas,
		MaxOpenConns:    sqliteMaxOpenConns,
		MaxIdleConns:    sqliteMaxIdleConns,
		ConnMaxLifetime: sqliteConnMaxLifetime,
		// DEFERRED 事务先读后写时可能遇到 SQLITE_BUSY_SNAPSHOT,
		// busy_timeout 对它无效; IMMEDIATE 在 BEGIN 时取写锁,
		// 让写事务排队等待而不是中途失败.
		TxLock: "immediate",
	}, &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	sqlDB, err := gormDB.DB.DB()
	if err != nil {
		return nil, err
	}
	return &DB{DB: gormDB.DB, Driver: config.DriverSQLite, sqlDB: sqlDB}, nil
}

func openViaGorm(ctx context.Context, driver string, dialector gorm.Dialector) (*DB, error) {
	gormDB, err := gorm.Open(dialector, &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", driver, err)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, err
	}
	// 启动时立即 Ping, 让连接配置错误在这里暴露, 而不是等到首个请求.
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping %s: %w", driver, err)
	}
	return &DB{DB: gormDB, Driver: driver, sqlDB: sqlDB}, nil
}
