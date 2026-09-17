// Package persistence 提供所有仓库共享的 GORM 辅助函数.
//
// 项目全程使用 Go 侧乐观锁: 更新携带期望 version, 命中后 version 加一;
// 影响行数不为 1 时返回 ErrVersionConflict, 由调用方重读后决定重试,
// 服务端不自动覆盖也不自动重试业务修改.
package persistence

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// ErrVersionConflict 表示条件更新未命中: 对象已被并发修改或已不存在.
var ErrVersionConflict = errors.New("version conflict")

// Now 返回当前 UTC 时间. 所有时间戳由 Go 生成, 不使用数据库默认值,
// 保证三个数据库的时间语义一致且不受时区配置影响.
func Now() time.Time { return time.Now().UTC() }

// UpdateOptimistic 按 id + expectedVersion 条件更新, 同时递增 version
// 并刷新 updated_at. dest 只用于取表名, 必须是模型结构体指针.
//
// fields 已显式提供 updated_at 时不再覆盖, 便于调用方在同一事务里
// 保持多个对象的时间一致.
func UpdateOptimistic(tx *gorm.DB, dest any, idValue any, expectedVersion int64, fields map[string]any) error {
	if fields == nil {
		fields = make(map[string]any, 2)
	}
	fields["version"] = gorm.Expr("version + 1")
	if _, ok := fields["updated_at"]; !ok {
		fields["updated_at"] = Now()
	}
	res := tx.Model(dest).
		Where("id = ? AND version = ?", idValue, expectedVersion).
		Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrVersionConflict
	}
	return nil
}

// UpdateConditional 执行非 version 键的条件更新, 返回是否恰好命中一行.
// embedding worker 的回写依赖它: 条件同时匹配领取任务时的 card version
// 和 processing 状态, front 被并发修改后旧任务自然写不进去.
func UpdateConditional(tx *gorm.DB, dest any, query string, args []any, fields map[string]any) (bool, error) {
	res := tx.Model(dest).Where(query, args...).Updates(fields)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}
