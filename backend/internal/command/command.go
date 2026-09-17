// Package command 实现 pi-teacher-server 的命令行入口:
// serve 子命令以及本机 migrate/admin 子命令.
package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
)

// ErrUsage 表示命令行用法错误, 进程以退出码 2 结束.
var ErrUsage = errors.New("用法错误")

const usage = `pi-teacher-server — Pi Teacher 单实例服务端

用法:
  pi-teacher-server serve   [--db-driver sqlite|mysql|postgres] --db-dsn DSN [--listen :8080] [--set key=value ...]
  pi-teacher-server migrate [--db-driver ...] --db-dsn DSN
  pi-teacher-server admin reset-password [--db-driver ...] --db-dsn DSN

说明:
  serve     启动 HTTP 服务, 执行迁移并初始化账号与设置
  migrate   只执行数据库迁移后退出
  admin     本机管理命令 (不做 HTTP 鉴权, 安全边界为操作系统权限)`

// Execute 分发子命令.
func Execute(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w\n\n%s", ErrUsage, usage)
	}
	switch args[0] {
	case "serve":
		return runServe(ctx, args[1:])
	case "migrate":
		return runMigrate(ctx, args[1:])
	case "admin":
		return runAdmin(ctx, args[1:])
	case "-h", "--help", "help":
		fmt.Fprintln(flag.CommandLine.Output(), usage)
		return nil
	default:
		return fmt.Errorf("%w: 未知子命令 %q\n\n%s", ErrUsage, args[0], usage)
	}
}
