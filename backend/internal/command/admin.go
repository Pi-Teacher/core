package command

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/Pi-Teacher/server/internal/application/appsvc"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/repo"
	"github.com/Pi-Teacher/server/internal/platform/config"
	"github.com/Pi-Teacher/server/internal/platform/database"
)

// runAdmin 分发本机管理子命令.
// 这类命令不做 HTTP 鉴权, 安全边界是能执行进程并读取数据库配置的操作系统用户.
func runAdmin(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: admin 需要子命令 (reset-password)", ErrUsage)
	}
	switch args[0] {
	case "reset-password":
		return runResetPassword(ctx, args[1:])
	default:
		return fmt.Errorf("%w: 未知 admin 子命令 %q", ErrUsage, args[0])
	}
}

// runResetPassword 重置密码并吊销全部 session.
func runResetPassword(ctx context.Context, args []string) error {
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
	accountRepo := repo.NewAccountRepository(db.DB)
	sessionRepo := repo.NewSessionRepository(db.DB)
	apiKeyRepo := repo.NewAPIKeyRepository(db.DB)
	// 管理命令不需要日志输出, 只保留 error 级防止意外刷屏.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	authSvc := appsvc.NewAuthService(accountRepo, sessionRepo, apiKeyRepo, logger)

	password, err := authSvc.ResetPassword(ctx)
	if err != nil {
		return err
	}
	// 明文密码只允许直达当前进程 stderr, 不进任何日志通道.
	fmt.Fprintf(os.Stderr,
		"\n密码已重置, 全部 session 已吊销.\n新密码 (仅本次显示, 请立即保存):\n\n    %s\n\n", password)
	return nil
}
