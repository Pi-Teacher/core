package command

import (
	"context"
	"fmt"

	"github.com/Pi-Teacher/server/internal/platform/config"
	"github.com/Pi-Teacher/server/internal/platform/database"
)

// runMigrate 只执行迁移后退出, 供部署脚本在启动服务前单独调用.
func runMigrate(ctx context.Context, args []string) error {
	cfg, err := config.Parse(args)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	db, err := database.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := database.Migrate(ctx, db); err != nil {
		return err
	}
	fmt.Printf("迁移完成 (%s)\n", cfg.DBDriver)
	return nil
}
