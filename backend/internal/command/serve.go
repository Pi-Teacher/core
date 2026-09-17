package command

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/Pi-Teacher/server/internal/application/appsvc"
	httpapi "github.com/Pi-Teacher/server/internal/delivery/http"
	"github.com/Pi-Teacher/server/internal/infrastructure/persistence/repo"
	"github.com/Pi-Teacher/server/internal/platform/config"
	"github.com/Pi-Teacher/server/internal/platform/database"
	"github.com/Pi-Teacher/server/internal/platform/logging"
	"github.com/Pi-Teacher/server/internal/platform/settings"
)

// runServe 启动 HTTP 服务: 连接数据库, 迁移, 初始化设置与账号, 然后监听.
// --set 在迁移完成后写入, 只影响命令行中显式出现的 key.
func runServe(ctx context.Context, args []string) error {
	cfg, err := config.Parse(args)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}

	// 运行期日志配置放在原子指针后, 设置变更后整体换新而不用重建 logger.
	runtimeCfg := &atomic.Pointer[logging.RuntimeConfig]{}
	runtimeCfg.Store(&logging.RuntimeConfig{
		StdoutLevel: slog.LevelInfo,
		DBEnabled:   false,
		DBLevel:     slog.LevelInfo,
	})

	db, err := database.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	logWriter := repo.NewAppLogWriter(db.DB)
	handler := logging.New(os.Stdout, runtimeCfg, logWriter)
	defer handler.Close()
	logger := slog.New(handler)

	if err := database.Migrate(ctx, db); err != nil {
		logger.ErrorContext(logging.WithEvent(ctx, "migration_failed"), "迁移失败", logging.AttrError, err.Error())
		return err
	}

	settingsRepo := repo.NewSettingsRepository(db.DB)
	if err := settingsRepo.EnsureDefaults(ctx); err != nil {
		return fmt.Errorf("初始化设置默认值: %w", err)
	}
	manager := settings.NewManager(settingsRepo)
	if err := manager.Refresh(ctx); err != nil {
		return fmt.Errorf("加载设置快照: %w", err)
	}
	applyRuntimeSettings(manager.Snapshot(), runtimeCfg)

	if err := applyStartupSets(ctx, manager, cfg.Sets); err != nil {
		return err
	}
	applyRuntimeSettings(manager.Snapshot(), runtimeCfg)

	accountRepo := repo.NewAccountRepository(db.DB)
	sessionRepo := repo.NewSessionRepository(db.DB)
	apiKeyRepo := repo.NewAPIKeyRepository(db.DB)
	authSvc := appsvc.NewAuthService(accountRepo, sessionRepo, apiKeyRepo, logger)

	password, created, err := authSvc.EnsureInitialAccount(ctx)
	if err != nil {
		return fmt.Errorf("初始化账号: %w", err)
	}
	if created {
		// 明文密码只允许直达 stderr, 不经过任何可能落库的 slog handler.
		fmt.Fprintf(os.Stderr,
			"\n首次启动已创建账号.\n初始密码 (仅本次显示, 请立即保存):\n\n    %s\n\n"+
				"提示: 忘记密码可在服务器本机执行 `pi-teacher-server admin reset-password`.\n\n",
			password)
	}

	startedAt := time.Now()
	router := httpapi.NewRouter(httpapi.RouterConfig{
		Auth:      authSvc,
		Logger:    logger,
		DBDriver:  db.Driver,
		StartedAt: startedAt,
	})

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.InfoContext(logging.WithEvent(ctx, "server_started"),
			"服务启动",
			"driver", db.Driver,
			"listen", cfg.Listen,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case <-ctx.Done():
		logger.InfoContext(logging.WithEvent(context.Background(), "server_stopping"), "收到停止信号, 正在关闭")
	case err := <-serveErr:
		if err != nil {
			return err
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("关闭 HTTP 服务: %w", err)
	}
	logger.InfoContext(logging.WithEvent(context.Background(), "server_stopped"), "服务已停止")
	return nil
}

// applyRuntimeSettings 把设置快照中的日志配置拷贝进原子运行期配置.
func applyRuntimeSettings(snap *settings.Snapshot, dst *atomic.Pointer[logging.RuntimeConfig]) {
	dst.Store(&logging.RuntimeConfig{
		StdoutLevel: logging.LevelMapping(snap.String("stdout_log_level")),
		DBEnabled:   snap.Bool("database_log_enabled"),
		DBLevel:     logging.LevelMapping(snap.String("database_log_level")),
	})
}

// applyStartupSets 持久化 --set 设置.
// 只处理命令行中显式出现的 key; 未注册或不允许命令行修改的 key 直接报错,
// 不静默忽略, 避免用户以为设置已生效.
func applyStartupSets(ctx context.Context, manager *settings.Manager, sets []config.Set) error {
	if len(sets) == 0 {
		return nil
	}
	updates := make([]settings.Update, 0, len(sets))
	for _, s := range sets {
		meta, ok := manager.Registry().Lookup(s.Key)
		if !ok {
			return fmt.Errorf("--set %s: 未知设置 key", s.Key)
		}
		if !meta.ServeSet {
			return fmt.Errorf("--set %s: 该设置不允许通过命令行修改", s.Key)
		}
		updates = append(updates, settings.Update{Key: s.Key, Value: s.Value})
	}
	if err := manager.Apply(ctx, updates); err != nil {
		return fmt.Errorf("应用 --set 设置: %w", err)
	}
	return nil
}
