package httpapi

import (
	"encoding/json"
	"fmt"
)

// NullableInt64 是三态整数: 区分 "字段缺省" 与 "显式 null".
// PATCH card 的 topic_id 需要三态语义: 缺省表示不修改,
// null 表示设为无 Topic, 数字表示指定 Topic.
type NullableInt64 struct {
	// Set 为 true 表示请求中出现了该字段.
	Set bool
	// Value 在显式 null 时为 nil.
	Value *int64
}

// UnmarshalJSON 记录字段是否出现, 并区分 null 与数字.
func (n *NullableInt64) UnmarshalJSON(data []byte) error {
	n.Set = true
	if string(data) == "null" {
		n.Value = nil
		return nil
	}
	var v int64
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("必须是整数或 null: %w", err)
	}
	n.Value = &v
	return nil
}
