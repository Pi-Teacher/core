// Command pi-teacher-server 是单实例 Pi Teacher 服务端及其本机管理 CLI.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Pi-Teacher/server/internal/command"
)

func main() {
	// 信号触发 context 取消, 各子命令据此走优雅退出.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := command.Execute(ctx, os.Args[1:]); err != nil {
		if errors.Is(err, command.ErrUsage) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}
