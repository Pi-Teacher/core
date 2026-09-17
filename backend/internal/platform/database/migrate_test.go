package database_test

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/Pi-Teacher/server/internal/infrastructure/persistence"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/model"
	"github.com/Pi-Teacher/server/internal/platform/config"
	"github.com/Pi-Teacher/server/internal/platform/database"
)

// expectedTables 列出全部 16 张表, 含 goose 的版本记录表.
var expectedTables = []string{
	"account",
	"api_key",
	"app_log",
	"approval_request",
	"approval_target",
	"calendar",
	"card",
	"card_schedule",
	"glossary",
	"goose_db_version",
	"idempotency_record",
	"review_log",
	"setting_keys",
	"topic",
	"trashed_card",
	"web_session",
}

func openMigrated(t *testing.T) *database.DB {
	t.Helper()
	ctx := context.Background()
	cfg := &config.Config{
		DBDriver: config.DriverSQLite,
		DBDSN:    filepath.Join(t.TempDir(), "m.db"),
		Listen:   ":0",
	}
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// TestMigrateCreatesAllTables 验证迁移后表集合与预期完全一致.
func TestMigrateCreatesAllTables(t *testing.T) {
	db := openMigrated(t)

	for _, m := range model.Models() {
		if !db.Migrator().HasTable(m) {
			t.Errorf("missing table for model %T", m)
		}
	}

	rows, err := db.SQL().QueryContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		// sqlite_sequence 是 AUTOINCREMENT 的内部表, 不属于业务表.
		if name == "sqlite_sequence" {
			continue
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)

	if len(names) != len(expectedTables) {
		t.Fatalf("table count = %d, want %d: %v", len(names), len(expectedTables), names)
	}
	for i, want := range expectedTables {
		if names[i] != want {
			t.Fatalf("table[%d] = %q, want %q", i, names[i], want)
		}
	}
}

// TestMigrateIsIdempotent 验证重复执行迁移是空操作.
func TestMigrateIsIdempotent(t *testing.T) {
	db := openMigrated(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

// TestNoForeignKeyConstraints 验证 DDL 中没有任何外键:
// 项目统一使用 Go 逻辑外键, 迁移里混入数据库外键属于实现错误.
func TestNoForeignKeyConstraints(t *testing.T) {
	db := openMigrated(t)
	rows, err := db.SQL().QueryContext(context.Background(),
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND sql IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var ddl string
		if err := rows.Scan(&ddl); err != nil {
			t.Fatal(err)
		}
		upper := strings.ToUpper(ddl)
		if strings.Contains(upper, "FOREIGN KEY") || strings.Contains(upper, "REFERENCES") {
			t.Errorf("unexpected foreign key in DDL: %s", ddl)
		}
	}
}

// TestOptimisticLockHelper 验证乐观锁 helper:
// 命中时递增 version, 期望版本过期时返回冲突且不覆盖新值.
func TestOptimisticLockHelper(t *testing.T) {
	db := openMigrated(t)
	now := persistence.Now()

	topic := model.Topic{Name: "A", Description: "", Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&topic).Error; err != nil {
		t.Fatal(err)
	}

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return persistence.UpdateOptimistic(tx, &model.Topic{}, topic.ID, 1, map[string]any{"name": "B"})
	})
	if err != nil {
		t.Fatalf("first optimistic update: %v", err)
	}

	// 仍用旧版本更新, 必须冲突.
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		return persistence.UpdateOptimistic(tx, &model.Topic{}, topic.ID, 1, map[string]any{"name": "C"})
	})
	if err != persistence.ErrVersionConflict {
		t.Fatalf("stale optimistic update error = %v, want ErrVersionConflict", err)
	}

	var reloaded model.Topic
	if err := db.DB.First(&reloaded, topic.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reloaded.Version != 2 || reloaded.Name != "B" {
		t.Fatalf("after update: version=%d name=%q, want version=2 name=B", reloaded.Version, reloaded.Name)
	}
}
