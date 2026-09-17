package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Pi-Teacher/server/internal/application/apperr"
)

// DefaultPageSize 与 MaxPageSize 约束 offset 分页.
// 单实例数据量小, offset 足够, 不引入 cursor.
const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// parsePaging 读取 page/page_size.
// 非法值回退默认而不是报错, 让分页参数永远可用; 超上限截断.
func parsePaging(r *http.Request) (page, pageSize int) {
	page = 1
	pageSize = DefaultPageSize
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			page = n
		}
	}
	if v := r.URL.Query().Get("page_size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			if n > MaxPageSize {
				n = MaxPageSize
			}
			pageSize = n
		}
	}
	return page, pageSize
}

// pathInt64 提取并解析 {id} 路径参数, 非正整数按校验错误处理.
func pathInt64(r *http.Request, name string) (int64, error) {
	raw := strings.TrimSpace(r.PathValue(name))
	if raw == "" {
		return 0, apperr.Validation("缺少路径参数 " + name)
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.Validation("路径参数 " + name + " 必须是正整数")
	}
	return id, nil
}
