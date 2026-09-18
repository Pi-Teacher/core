package appsvc

import (
	"context"

	"gorm.io/gorm"

	"github.com/Pi-Teacher/server/internal/application/apperr"
)

// BatchMaxItems 是单次批量请求的项目上限, 与 API 设计的批量语义一致.
const BatchMaxItems = 100

// runBatch 在单个数据库事务中逐项执行 fn, 任一项失败整批回滚,
// 并把失败项目的下标附加到错误的 details.index, 让客户端精确定位.
//
// 空批与超上限在开事务前拒绝, 避免无意义的事务开销.
func runBatch[T any](ctx context.Context, db *gorm.DB, items []T, fn func(tx *gorm.DB, item T) error) error {
	if len(items) == 0 {
		return apperr.Validation("items 不能为空").WithDetails(map[string]any{"field": "items"})
	}
	if len(items) > BatchMaxItems {
		return apperr.Newf(apperr.CodeValidationError, "批量项目数超过上限 %d", BatchMaxItems).
			WithDetails(map[string]any{"field": "items"})
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i, item := range items {
			if err := fn(tx, item); err != nil {
				return withBatchIndex(err, i)
			}
		}
		return nil
	})
}

// withBatchIndex 给错误附加 details.index. 已是应用错误时原地补充,
// 避免再包一层; 未知错误按内部错误处理, 不泄漏底层细节.
func withBatchIndex(err error, index int) error {
	if appErr := apperr.As(err); appErr != nil {
		if appErr.Details == nil {
			appErr.Details = map[string]any{}
		}
		appErr.Details["index"] = index
		return appErr
	}
	return apperr.Wrap(apperr.CodeInternal, "批量操作失败", err).
		WithDetails(map[string]any{"index": index})
}
