// Package migrations 以 go:embed 内嵌三种数据库的版本化 DDL.
//
// 按方言分目录存放, 运行时只加载当前驱动的子目录: 三套迁移互不影响,
// 版本号在各自目录内独立递增, 不要求跨方言对齐文件内容.
package migrations

import "embed"

// FS 持有全部方言的 goose 迁移文件.
//
//go:embed sqlite/*.sql mysql/*.sql postgres/*.sql
var FS embed.FS

// DialectDir 返回驱动名对应的迁移子目录名.
func DialectDir(driver string) string {
	switch driver {
	case "sqlite":
		return "sqlite"
	case "mysql":
		return "mysql"
	case "postgres":
		return "postgres"
	default:
		return driver
	}
}
