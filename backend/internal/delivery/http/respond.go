// Package httpapi 实现 HTTP 交付层: 路由, 中间件和请求响应编解码.
// 只做协议转换, 不含业务规则.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Pi-Teacher/server/internal/application/apperr"
	"github.com/Pi-Teacher/server/internal/platform/logging"
)

// maxRequestBody 限制解码的 JSON 请求体大小, 防御异常客户端.
const maxRequestBody = 1 << 20 // 1 MiB

type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
	ctxKeySession
	ctxKeyAPIKey
)

// errorEnvelope 是统一的非 2xx 响应体.
type errorEnvelope struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// writeJSON 以 JSON 编码响应. 状态行写出后编码失败已无法补救, 只能放弃.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// writeError 把任意错误渲染成统一错误信封, 未知错误一律内部错误.
func writeError(w http.ResponseWriter, err error) {
	appErr := apperr.From(err)
	if appErr == nil {
		writeJSON(w, http.StatusInternalServerError, errorEnvelope{Error: errorDetail{
			Code: string(apperr.CodeInternal), Message: "内部错误",
		}})
		return
	}
	writeJSON(w, appErr.HTTPStatus(), errorEnvelope{Error: errorDetail{
		Code:    string(appErr.Code),
		Message: appErr.Message,
		Details: appErr.Details,
	}})
}

// decodeJSON 读取并解码 JSON 请求体. 禁止未知字段, 让客户端的拼写错误
// 尽早暴露而不是被静默忽略; 超过上限的体按校验错误拒绝.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Body == nil {
		return apperr.Validation("请求体不能为空")
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return apperr.Validation("请求体不能为空")
		}
		return apperr.Validation("请求体不是合法 JSON: " + err.Error())
	}
	return nil
}

// --- 请求 ID ---

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}

// RequestIDFromContext 返回请求 ID, 不存在时为空串.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return v
	}
	return ""
}

// --- 中间件 ---

// statusRecorder 记录实际响应状态, 供访问日志使用.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// withRequestID 生成请求 ID, 写入 context 和响应头, 便于日志与客户端对账.
// 优先沿用客户端带来的 X-Request-Id, 方便调用链串联.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), ctxKeyRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// withRecovery 把 panic 转成 500, 不让进程带着损坏状态继续服务.
func withRecovery(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.ErrorContext(logging.WithEvent(r.Context(), "http_panic"),
					"http 请求 panic", "panic", rec)
				writeError(w, apperr.New(apperr.CodeInternal, "内部错误"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// withAccessLog 按请求记录访问日志 (debug 级).
// 业务事件有自己的结构化日志, 这里只保留协议层观测.
func withAccessLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		logger.DebugContext(logging.WithEvent(r.Context(), "http_request"),
			"http 请求",
			logging.AttrRequestID, RequestIDFromContext(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
		)
	})
}

// trimBearer 提取 Bearer token, 格式不符返回 false.
func trimBearer(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}
