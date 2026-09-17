package settings

import (
	"fmt"
	"strconv"
	"time"
)

// Snapshot 是全部设置值的不可变视图.
type Snapshot struct {
	values map[string]string
}

// NewSnapshot 从原始字符串值构造快照.
// 未注册的 key 一律忽略, 缺失的已注册 key 回退默认值,
// 保证快照总能解析每一个已知 key, 调用方无需判空.
func NewSnapshot(values map[string]string) *Snapshot {
	merged := make(map[string]string, len(registry.order))
	for _, s := range registry.All() {
		merged[s.Key] = s.Default
	}
	for k, v := range values {
		if _, ok := registry.Lookup(k); ok {
			merged[k] = v
		}
	}
	return &Snapshot{values: merged}
}

// String 返回 key 的原始字符串值.
func (s *Snapshot) String(key string) string { return s.values[key] }

// Bool 把 key 解析为 bool. 值入库前已经过校验, 解析失败只可能是
// 数据被绕过服务层手工改坏, 此时按零值处理并依赖后续写覆盖纠正.
func (s *Snapshot) Bool(key string) bool {
	v, _ := strconv.ParseBool(s.values[key])
	return v
}

// Int64 把 key 解析为 int64, 失败时返回 0.
func (s *Snapshot) Int64(key string) int64 {
	v, _ := strconv.ParseInt(s.values[key], 10, 64)
	return v
}

// Float64 把 key 解析为 float64, 失败时返回 0.
func (s *Snapshot) Float64(key string) float64 {
	v, _ := strconv.ParseFloat(s.values[key], 64)
	return v
}

// Timezone 解析 calendar_timezone. 非法值回退 UTC:
// 时区函数绝不能返回 nil, 否则每个调用点都要处理空指针.
func (s *Snapshot) Timezone() *time.Location {
	loc, err := time.LoadLocation(s.values["calendar_timezone"])
	if err != nil {
		return time.UTC
	}
	return loc
}

// Values 返回原始值 map 的副本, 避免调用方绕过快照直接改内部状态.
func (s *Snapshot) Values() map[string]string {
	out := make(map[string]string, len(s.values))
	for k, v := range s.values {
		out[k] = v
	}
	return out
}

// MarshalValue 按注册表类型编码并校验一个设置值, 返回编码值和值类型.
// 编码与校验集中在这里, 仓库层只负责存取, 不重复实现规则.
func (r *Registry) MarshalValue(key, raw string) (string, ValueType, error) {
	s, ok := r.Lookup(key)
	if !ok {
		return "", 0, fmt.Errorf("unknown setting key %q", key)
	}
	encoded, err := encode(s.Type, raw)
	if err != nil {
		return "", 0, err
	}
	if s.Validate != nil {
		if err := s.Validate(encoded); err != nil {
			return "", 0, fmt.Errorf("setting %s: %w", key, err)
		}
	}
	return encoded, s.Type, nil
}

// encode 把原始字符串规范化为该类型的存储编码,
// 例如布尔统一为 true/false, 整数统一为十进制, 保证同值同串.
func encode(t ValueType, raw string) (string, error) {
	switch t {
	case TypeString, TypeJSON:
		return raw, nil
	case TypeBool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return "", fmt.Errorf("不是合法布尔值: %q", raw)
		}
		return strconv.FormatBool(b), nil
	case TypeInt64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return "", fmt.Errorf("不是合法整数: %q", raw)
		}
		return strconv.FormatInt(n, 10), nil
	case TypeFloat64:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return "", fmt.Errorf("不是合法浮点数: %q", raw)
		}
		return strconv.FormatFloat(f, 'g', -1, 64), nil
	default:
		return "", fmt.Errorf("未知设置类型 %d", t)
	}
}
