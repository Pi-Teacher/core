package httpapi

import (
	"net/http"
	"strconv"
)

// parseOptionalTopicID 解析可选的 topic_id 查询参数.
// 缺省返回 nil (不过滤); 0 返回指向 0 的指针 (表示无 Topic); 其余必须非负.
func parseOptionalTopicID(r *http.Request) (*int64, error) {
	v := r.URL.Query().Get("topic_id")
	if v == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return nil, validationFieldError("topic_id", "必须是非负整数")
	}
	return &n, nil
}

// parseDueLimit 解析复习队列的 limit 参数.
// 缺省返回 0 (服务层取默认值); 非法值报校验错误.
func parseDueLimit(r *http.Request) (int, error) {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, validationFieldError("limit", "必须是正整数")
	}
	return n, nil
}
