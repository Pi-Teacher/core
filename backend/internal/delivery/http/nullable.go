package httpapi

import "github.com/Pi-Teacher/server/internal/application/appsvc"

// NullableInt64 复用应用层定义的三态整数, HTTP 请求解码与审批 payload
// 使用同一实现, 避免两处语义漂移.
type NullableInt64 = appsvc.NullableInt64
