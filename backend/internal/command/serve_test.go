package command

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/repo"
	"github.com/Pi-Teacher/server/internal/platform/config"
	"github.com/Pi-Teacher/server/internal/platform/database"
	"github.com/Pi-Teacher/server/internal/platform/settings"
)

// TestApplyStartupSets 验证 serve --set 只写入命令行显式出现的 key,
// 且未知 key 与不允许命令行修改的 key 直接报错.
func TestApplyStartupSets(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(ctx, &config.Config{
		DBDriver: config.DriverSQLite, DBDSN: dbPath, Listen: ":0",
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repo.NewSettingsRepository(db.DB)
	if err := repo.EnsureDefaults(ctx); err != nil {
		t.Fatalf("ensure defaults: %v", err)
	}
	manager := settings.NewManager(repo)
	if err := manager.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	// 默认开启, --set 关闭其中一个.
	sets := []config.Set{
		{Key: "enable_cli_card_create_approval", Value: "false"},
		{Key: "database_max_rows", Value: "42"},
	}
	if err := applyStartupSets(ctx, manager, sets); err != nil {
		t.Fatalf("applyStartupSets: %v", err)
	}
	if manager.Snapshot().Bool("enable_cli_card_create_approval") {
		t.Fatal("card create approval should be disabled")
	}
	if got := manager.Snapshot().Int64("database_max_rows"); got != 42 {
		t.Fatalf("database_max_rows = %d, want 42", got)
	}
	// 未在 --set 中出现的 key 保持默认.
	if !manager.Snapshot().Bool("enable_cli_card_update_approval") {
		t.Fatal("untouched key should keep default")
	}

	// 未知 key 报错.
	if err := applyStartupSets(ctx, manager, []config.Set{{Key: "nope", Value: "1"}}); err == nil {
		t.Fatal("unknown key should error")
	}
	// 不允许命令行修改的 key 报错.
	if err := applyStartupSets(ctx, manager, []config.Set{{Key: "user_profile", Value: "x"}}); err == nil {
		t.Fatal("non-ServeSet key should error")
	}
}
