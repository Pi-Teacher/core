// Package apperr 定义统一的 API 错误类型与错误码.
// 所有非 2xx 响应都使用这里的错误码, 保证客户端可以按 code 稳定分支.
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

// Code 是稳定的机器可读错误码.
type Code string

const (
	CodeValidationError     Code = "validation_error"
	CodeUnauthorized        Code = "unauthorized"
	CodeForbidden           Code = "forbidden"
	CodeNotFound            Code = "not_found"
	CodeVersionConflict     Code = "version_conflict"
	CodeNameConflict        Code = "name_conflict"
	CodeMergeTopicRequired  Code = "merge_topic_required"
	CodeMergeEmbeddingReqd  Code = "merge_embedding_required"
	CodeRebuilding          Code = "rebuilding"
	CodeSimilarityDisabled  Code = "similarity_disabled"
	CodeIdempotencyConflict Code = "idempotency_conflict"
	CodePayloadTooLarge     Code = "payload_too_large"
	CodeApprovalNotPending  Code = "approval_not_pending"
	CodeRateLimited         Code = "rate_limited"
	CodeEmbeddingUnavail    Code = "embedding_unavailable"
	CodeInternal            Code = "internal_error"
)

// statusFor 把错误码映射到 HTTP 状态码.
var statusFor = map[Code]int{
	CodeValidationError:     http.StatusBadRequest,
	CodeUnauthorized:        http.StatusUnauthorized,
	CodeForbidden:           http.StatusForbidden,
	CodeNotFound:            http.StatusNotFound,
	CodeVersionConflict:     http.StatusConflict,
	CodeNameConflict:        http.StatusConflict,
	CodeMergeTopicRequired:  http.StatusConflict,
	CodeMergeEmbeddingReqd:  http.StatusConflict,
	CodeRebuilding:          http.StatusConflict,
	CodeSimilarityDisabled:  http.StatusConflict,
	CodeIdempotencyConflict: http.StatusConflict,
	CodePayloadTooLarge:     http.StatusRequestEntityTooLarge,
	CodeApprovalNotPending:  http.StatusUnprocessableEntity,
	CodeRateLimited:         http.StatusTooManyRequests,
	CodeEmbeddingUnavail:    http.StatusServiceUnavailable,
	CodeInternal:            http.StatusInternalServerError,
}

// Error 携带错误码, 人类可读消息和可选结构化详情.
type Error struct {
	Code    Code
	Message string
	Details map[string]any
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

// HTTPStatus 返回错误码对应的 HTTP 状态, 未知码按内部错误处理.
func (e *Error) HTTPStatus() int {
	if s, ok := statusFor[e.Code]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// New 构造指定错误码与消息的错误.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Newf 构造格式化消息的错误.
func Newf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap 包装底层错误并附带错误码与消息.
func Wrap(code Code, message string, err error) *Error {
	return &Error{Code: code, Message: message, Err: err}
}

// WithDetails 附加结构化详情并返回自身, 便于链式构造.
func (e *Error) WithDetails(details map[string]any) *Error {
	e.Details = details
	return e
}

// 高频错误码的构造函数, 让调用点保持一行.

func Validation(message string) *Error { return New(CodeValidationError, message) }

func Unauthorized(message string) *Error { return New(CodeUnauthorized, message) }

func Forbidden(message string) *Error { return New(CodeForbidden, message) }

func NotFound(message string) *Error { return New(CodeNotFound, message) }

func Conflict(code Code, message string) *Error { return New(code, message) }

// As 从 err 中提取 *Error, 不是该类型时返回 nil.
func As(err error) *Error {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return nil
}

// From 把任意错误转换为 *Error, 未知错误一律按内部错误处理,
// 避免底层错误细节直接泄漏给客户端.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	if appErr := As(err); appErr != nil {
		return appErr
	}
	return Wrap(CodeInternal, "内部错误", err)
}
