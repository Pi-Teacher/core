package httpapi

import (
	"github.com/Pi-Teacher/server/internal/application/apperr"
)

// requireExpectedVersion 提取必填的乐观锁期望版本.
// 所有携带乐观锁的写请求都要求客户端显式提供, 缺失按校验错误返回.
func requireExpectedVersion(v *int64) (int64, error) {
	if v == nil {
		return 0, apperr.Validation("缺少 expected_version").
			WithDetails(map[string]any{"field": "expected_version"})
	}
	return *v, nil
}
