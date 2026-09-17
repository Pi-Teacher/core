// Package config 解析 pi-teacher-server 的启动配置.
//
// 数据库驱动, DSN 和监听地址必须在连接数据库之前确定, 因此它们是命令行
// 参数而不是 setting_keys 里的运行期设置. --set 可以重复出现, 只携带本次
// 启动显式给出的设置项, 迁移完成后才写入数据库, 未出现的 key 保持原值.
package config

import (
	"errors"
	"flag"
	"fmt"
	"strings"
)

// Driver names 是 --db-driver 接受的取值.
const (
	DriverSQLite   = "sqlite"
	DriverMySQL    = "mysql"
	DriverPostgres = "postgres"
)

// Config 是解析完成的启动配置.
type Config struct {
	DBDriver string
	DBDSN    string
	Listen   string
	// Sets 只包含命令行中显式出现的 --set 项, 保持出现顺序.
	Sets []Set
}

// Set 是一个 --set 提供的 key=value 对.
type Set struct {
	Key   string
	Value string
}

// stringList 收集可重复出现的 flag 值.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// Parse 解析 serve 参数, args 不包含子命令名.
func Parse(args []string) (*Config, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(newDiscardWriter())

	var (
		driver string
		dsn    string
		listen string
		raws   stringList
	)
	fs.StringVar(&driver, "db-driver", DriverSQLite, "database driver: sqlite|mysql|postgres")
	fs.StringVar(&dsn, "db-dsn", "", "database DSN or SQLite file path")
	fs.StringVar(&listen, "listen", ":8080", "HTTP listen address")
	fs.Var(&raws, "set", "persist a setting: --set key=value (repeatable)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	cfg := &Config{
		DBDriver: strings.ToLower(strings.TrimSpace(driver)),
		DBDSN:    strings.TrimSpace(dsn),
		Listen:   strings.TrimSpace(listen),
	}
	switch cfg.DBDriver {
	case DriverSQLite, DriverMySQL, DriverPostgres:
	default:
		return nil, fmt.Errorf("unsupported --db-driver %q (want sqlite, mysql or postgres)", driver)
	}
	if cfg.DBDSN == "" {
		return nil, errors.New("--db-dsn is required")
	}
	if cfg.Listen == "" {
		return nil, errors.New("--listen must not be empty")
	}

	for _, raw := range raws {
		key, value, ok := strings.Cut(raw, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --set %q: want key=value", raw)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("invalid --set %q: empty key", raw)
		}
		cfg.Sets = append(cfg.Sets, Set{Key: key, Value: value})
	}
	return cfg, nil
}

// discardWriter 丢弃 flag 包自带的用法输出, 错误信息由本包格式化后返回给调用方.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func newDiscardWriter() discardWriter { return discardWriter{} }
