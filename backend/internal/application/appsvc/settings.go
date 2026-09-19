package appsvc

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/platform/settings"
)

// SettingsService 暴露通用设置端点的可读写范围与类型化编解码.
//
// 只暴露 approval / calendar / log 三组: embedding 与 user_profile 各有
// 独立资源端点 (含乐观锁或敏感值), embedding_rebuilding 等内部状态不
// 通过通用端点修改. 范围外的 key 一律按未知 key 拒绝, 而不是静默忽略.
type SettingsService struct {
	manager *settings.Manager
	// onApply 在写入成功后触发, 用于刷新运行期配置 (如日志级别).
	// 为 nil 时跳过.
	onApply func(snapshot *settings.Snapshot)
}

// NewSettingsService 构造设置服务. onApply 可选.
func NewSettingsService(manager *settings.Manager, onApply func(*settings.Snapshot)) *SettingsService {
	return &SettingsService{manager: manager, onApply: onApply}
}

// exposedGroups 是通用设置端点允许读写与展示的分组.
var exposedGroups = []settings.Group{
	settings.GroupApproval,
	settings.GroupCalendar,
	settings.GroupLog,
}

// exposedKeys 返回按注册顺序排列的范围内 key.
func (s *SettingsService) exposedKeys() []string {
	var keys []string
	registry := s.manager.Registry()
	for _, g := range exposedGroups {
		for _, item := range registry.Group(g) {
			keys = append(keys, item.Key)
		}
	}
	return keys
}

// Snapshot 返回当前设置快照, 供上层读取审批开关等.
func (s *SettingsService) Snapshot() *settings.Snapshot { return s.manager.Snapshot() }

// ApprovalEnabled 返回某审批开关当前是否为开.
func (s *SettingsService) ApprovalEnabled(key string) bool {
	return s.manager.Snapshot().Bool(key)
}

// Values 返回范围内设置的类型化值, 供 GET /api/web/settings 直接编码为 JSON.
func (s *SettingsService) Values() map[string]any {
	snap := s.manager.Snapshot()
	out := make(map[string]any, len(s.exposedKeys()))
	for _, key := range s.exposedKeys() {
		out[key] = s.typedValue(key, snap)
	}
	return out
}

// typedValue 按登记类型输出 JSON 值: bool 为布尔, 整数为数字, 其余为字符串.
// 快照中的值都经过校验, 解析失败只可能来自绕过服务层的手工改动,
// 此时按零值输出, 后续写会覆盖纠正.
func (s *SettingsService) typedValue(key string, snap *settings.Snapshot) any {
	meta, _ := s.manager.Registry().Lookup(key)
	switch meta.Type {
	case settings.TypeBool:
		return snap.Bool(key)
	case settings.TypeInt64:
		return snap.Int64(key)
	case settings.TypeFloat64:
		return snap.Float64(key)
	default:
		return snap.String(key)
	}
}

// Patch 按范围内 key 写入设置. 请求体是 JSON 对象的 key-value 子集,
// 值类型必须与登记类型一致: bool 对应 JSON 布尔, 整数对应 JSON 数字
// (或可解析的字符串), 字符串对应 JSON 字符串. 范围外 key 报未知 key.
func (s *SettingsService) Patch(ctx context.Context, raw map[string]json.RawMessage) error {
	if len(raw) == 0 {
		return apperr.Validation("请求体不能为空")
	}
	allowed := make(map[string]settings.Setting, len(s.exposedKeys()))
	for _, key := range s.exposedKeys() {
		meta, _ := s.manager.Registry().Lookup(key)
		allowed[key] = meta
	}
	updates := make([]settings.Update, 0, len(raw))
	for key, rawValue := range raw {
		meta, ok := allowed[key]
		if !ok {
			return apperr.Validation("未知设置 key: " + key).
				WithDetails(map[string]any{"field": key})
		}
		value, err := decodeSettingValue(meta.Type, rawValue)
		if err != nil {
			return apperr.Validation(key + " 值类型不匹配: " + err.Error()).
				WithDetails(map[string]any{"field": key})
		}
		updates = append(updates, settings.Update{Key: key, Value: value})
	}
	// 排序让同一批写入的顺序稳定, 便于测试与日志.
	sortUpdates(updates, s.exposedKeys())
	if err := s.manager.Apply(ctx, updates); err != nil {
		return apperr.Validation(err.Error())
	}
	if s.onApply != nil {
		s.onApply(s.manager.Snapshot())
	}
	return nil
}

// sortUpdates 按注册顺序排列更新, 保证写入顺序可预期.
func sortUpdates(updates []settings.Update, order []string) {
	index := make(map[string]int, len(order))
	for i, key := range order {
		index[key] = i
	}
	sort.SliceStable(updates, func(i, j int) bool {
		return index[updates[i].Key] < index[updates[j].Key]
	})
}

// decodeSettingValue 把 JSON 值转为设置存储的字符串编码,
// 类型与登记类型不一致时报错.
func decodeSettingValue(t settings.ValueType, raw json.RawMessage) (string, error) {
	switch t {
	case settings.TypeBool:
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return "", fmt.Errorf("应为布尔值")
		}
		return strconv.FormatBool(b), nil
	case settings.TypeInt64:
		var n int64
		if err := json.Unmarshal(raw, &n); err != nil {
			// 兼容客户端用字符串传数字.
			var s string
			if err2 := json.Unmarshal(raw, &s); err2 != nil {
				return "", fmt.Errorf("应为整数")
			}
			if _, err3 := strconv.ParseInt(s, 10, 64); err3 != nil {
				return "", fmt.Errorf("应为整数")
			}
			return s, nil
		}
		return strconv.FormatInt(n, 10), nil
	case settings.TypeFloat64:
		var f float64
		if err := json.Unmarshal(raw, &f); err != nil {
			return "", fmt.Errorf("应为浮点数")
		}
		return strconv.FormatFloat(f, 'g', -1, 64), nil
	default:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", fmt.Errorf("应为字符串")
		}
		return s, nil
	}
}
