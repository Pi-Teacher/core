// Package settings 定义 setting_keys 的已知 key 注册表, 各值类型的字符串
// 编解码, 以及可原子替换的类型化内存快照.
//
// 数据库是权威来源: 启动时加载进快照, 更新成功后整体原子替换,
// 常规请求只读快照而不查表.
package settings

import (
	"fmt"
	"strconv"
	"time"
)

// ValueType 枚举 setting_keys.value_type 的取值.
type ValueType int16

const (
	TypeString  ValueType = 1
	TypeBool    ValueType = 2
	TypeInt64   ValueType = 3
	TypeFloat64 ValueType = 4
	TypeJSON    ValueType = 5
)

// Group 把设置按用途分组, 供 API 分区暴露.
type Group string

const (
	GroupApproval  Group = "approval"
	GroupCalendar  Group = "calendar"
	GroupLog       Group = "log"
	GroupEmbedding Group = "embedding"
	GroupProfile   Group = "profile"
	GroupInternal  Group = "internal"
)

// Setting 描述一个已注册的设置 key.
type Setting struct {
	Key   string
	Type  ValueType
	Group Group
	// Default 是该 key 缺行时使用的字符串编码默认值.
	Default string
	// Sensitive 标记不可进入日志的值.
	Sensitive bool
	// ServeSet 标记是否允许 serve --set 修改该 key.
	ServeSet bool
	// Validate 校验原始字符串值, nil 表示接受任意字符串.
	Validate    func(string) error
	Description string
}

// Registry 是不可变的已知设置集合.
type Registry struct {
	order []string
	byKey map[string]Setting
}

// Lookup 返回 key 的设置元数据.
func (r *Registry) Lookup(key string) (Setting, bool) {
	s, ok := r.byKey[key]
	return s, ok
}

// Keys 按声明顺序返回全部已注册 key.
func (r *Registry) Keys() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// All 按声明顺序返回全部设置.
func (r *Registry) All() []Setting {
	out := make([]Setting, 0, len(r.order))
	for _, k := range r.order {
		out = append(out, r.byKey[k])
	}
	return out
}

// Group 按声明顺序返回属于 g 的设置.
func (r *Registry) Group(g Group) []Setting {
	var out []Setting
	for _, k := range r.order {
		if r.byKey[k].Group == g {
			out = append(out, r.byKey[k])
		}
	}
	return out
}

var registry = buildRegistry()

// Default 返回进程级注册表.
func Default() *Registry { return registry }

// buildRegistry 声明全部已知设置. 重复 key 直接 panic,
// 让编码错误在启动时立即暴露而不是静默覆盖.
func buildRegistry() *Registry {
	r := &Registry{byKey: map[string]Setting{}}
	add := func(s Setting) {
		if _, dup := r.byKey[s.Key]; dup {
			panic("settings: duplicate key " + s.Key)
		}
		r.byKey[s.Key] = s
		r.order = append(r.order, s.Key)
	}

	// 13 个 CLI 审批开关, 默认全部开启: agent 的提议必须先经用户审批,
	// 用户显式关闭某类操作后才会直写.
	for _, key := range []string{
		"enable_cli_card_create_approval",
		"enable_cli_card_update_approval",
		"enable_cli_card_trash_approval",
		"enable_cli_card_restore_approval",
		"enable_cli_card_merge_approval",
		"enable_cli_topic_create_approval",
		"enable_cli_topic_update_approval",
		"enable_cli_topic_trash_approval",
		"enable_cli_topic_restore_approval",
		"enable_cli_glossary_create_approval",
		"enable_cli_glossary_update_approval",
		"enable_cli_glossary_trash_approval",
		"enable_cli_glossary_restore_approval",
	} {
		add(Setting{
			Key: key, Type: TypeBool, Group: GroupApproval,
			Default: "true", ServeSet: true, Validate: validateBool,
			Description: "CLI 对应操作是否需要用户审批",
		})
	}

	add(Setting{
		Key: "calendar_timezone", Type: TypeString, Group: GroupCalendar,
		Default: "UTC", ServeSet: true, Validate: validateTimezone,
		Description: "IANA 时区名, 用于自然日归属",
	})

	// 用户画像走独立资源端点并使用乐观锁, 不混入通用设置接口,
	// 但仍存在 setting_keys 中, 与其他设置共用加载与快照机制.
	add(Setting{
		Key: "user_profile", Type: TypeString, Group: GroupProfile,
		Default: "", ServeSet: false,
		Description: "Markdown 纯文本用户画像, 使用独立资源端点与乐观锁",
	})

	// Embedding 连接配置. 修改配置不自动重建向量, 重建由用户手动触发.
	add(Setting{
		Key: "embedding_base_url", Type: TypeString, Group: GroupEmbedding,
		Default: "", ServeSet: true,
		Description: "OpenAI-compatible embeddings base URL",
	})
	add(Setting{
		Key: "embedding_api_key", Type: TypeString, Group: GroupEmbedding,
		Default: "", ServeSet: true, Sensitive: true,
		Description: "Embedding API Key, 明文保存",
	})
	add(Setting{
		Key: "embedding_model", Type: TypeString, Group: GroupEmbedding,
		Default: "", ServeSet: true,
		Description: "Embedding 模型名",
	})
	add(Setting{
		Key: "embedding_dimensions", Type: TypeInt64, Group: GroupEmbedding,
		Default: "768", ServeSet: true, Validate: validatePositiveInt,
		Description: "向量维度",
	})
	add(Setting{
		Key: "embedding_timeout", Type: TypeInt64, Group: GroupEmbedding,
		Default: "30", ServeSet: true, Validate: validatePositiveInt,
		Description: "Embedding 请求超时秒数",
	})
	// rebuilding 与 similarity_enabled 是持久化的业务状态而不是配置:
	// 前者标记重建进行中, 后者由覆盖率达标与否驱动, 都不允许 --set 修改.
	add(Setting{
		Key: "embedding_rebuilding", Type: TypeBool, Group: GroupInternal,
		Default: "false", ServeSet: false, Validate: validateBool,
		Description: "完整重建进行中 (持久化业务状态)",
	})
	add(Setting{
		Key: "embedding_similarity_enabled", Type: TypeBool, Group: GroupEmbedding,
		Default: "false", ServeSet: false, Validate: validateBool,
		Description: "语义查重能力是否开放",
	})
	add(Setting{
		Key: "embedding_similarity_min_ready_percent", Type: TypeInt64, Group: GroupEmbedding,
		Default: "90", ServeSet: true, Validate: validatePercent,
		Description: "开放语义查重所需最低覆盖率",
	})
	add(Setting{
		Key: "embedding_worker_batch_size", Type: TypeInt64, Group: GroupEmbedding,
		Default: "100", ServeSet: true, Validate: validatePositiveInt,
		Description: "Embedding worker 每批处理数量",
	})

	add(Setting{
		Key: "stdout_log_level", Type: TypeString, Group: GroupLog,
		Default: "info", ServeSet: true, Validate: validateLogLevel,
		Description: "标准输出最低日志级别",
	})
	add(Setting{
		Key: "database_log_enabled", Type: TypeBool, Group: GroupLog,
		Default: "false", ServeSet: true, Validate: validateBool,
		Description: "是否将关键日志写入 app_log",
	})
	add(Setting{
		Key: "database_log_level", Type: TypeString, Group: GroupLog,
		Default: "info", ServeSet: true, Validate: validateLogLevel,
		Description: "写入 app_log 的最低级别",
	})
	add(Setting{
		Key: "database_retention_days", Type: TypeInt64, Group: GroupLog,
		Default: "30", ServeSet: true, Validate: validatePositiveInt,
		Description: "app_log 保留天数",
	})
	add(Setting{
		Key: "database_max_rows", Type: TypeInt64, Group: GroupLog,
		Default: "10000", ServeSet: true, Validate: validatePositiveInt,
		Description: "app_log 最大行数",
	})

	return r
}

// --- 校验器 ---

func validateBool(v string) error {
	if _, err := strconv.ParseBool(v); err != nil {
		return fmt.Errorf("不是合法布尔值: %q", v)
	}
	return nil
}

func validatePositiveInt(v string) error {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fmt.Errorf("不是合法整数: %q", v)
	}
	if n <= 0 {
		return fmt.Errorf("必须为正整数: %d", n)
	}
	return nil
}

func validatePercent(v string) error {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fmt.Errorf("不是合法整数: %q", v)
	}
	if n < 0 || n > 100 {
		return fmt.Errorf("百分比必须在 0..100: %d", n)
	}
	return nil
}

func validateTimezone(v string) error {
	if v == "" {
		return fmt.Errorf("时区不能为空")
	}
	if _, err := time.LoadLocation(v); err != nil {
		return fmt.Errorf("不是合法 IANA 时区: %q", v)
	}
	return nil
}

func validateLogLevel(v string) error {
	switch v {
	case "debug", "info", "warn", "error":
		return nil
	default:
		return fmt.Errorf("日志级别必须是 debug/info/warn/error: %q", v)
	}
}
