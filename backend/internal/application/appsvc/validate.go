package appsvc

import (
	"strings"

	"github.com/Pi-Teacher/server/internal/application/apperr"
)

// 文本限制与数据库设计的应用层限制一致.
const (
	// maxNameRunes 是 Topic.name / Glossary.term 的 Unicode 字符上限.
	maxNameRunes = 200
	// maxTextBytes 是 front/back/description 的 UTF-8 字节上限.
	maxTextBytes = 64 * 1024
)

// validateIdentifier 校验名称类字段 (Topic.name, Glossary.term):
// 返回去除首尾空白后的值, 要求非空且不超过 maxNameRunes 个 Unicode 字符.
// 名称是标识符而不是内容, 因此保存 trim 后的值, 避免首尾空白造成
// " Go " 与 "Go" 两个名字.
func validateIdentifier(field, raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", apperr.Validation(field + " 不能为空").
			WithDetails(map[string]any{"field": field})
	}
	if n := len([]rune(v)); n > maxNameRunes {
		return "", apperr.Newf(apperr.CodeValidationError, "%s 最多 %d 个字符", field, maxNameRunes).
			WithDetails(map[string]any{"field": field})
	}
	return v, nil
}

// validateContent 校验内容类字段 (Card.front, Card.back):
// 保存原始文本不改写, 只要求去除首尾空白后非空且不超过 64 KiB.
// fingerprint 同样基于原始文本计算.
func validateContent(field, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", apperr.Validation(field + " 不能为空").
			WithDetails(map[string]any{"field": field})
	}
	if len(raw) > maxTextBytes {
		return "", apperr.Newf(apperr.CodeValidationError, "%s 最大 64 KiB", field).
			WithDetails(map[string]any{"field": field})
	}
	return raw, nil
}

// validateOptionalText 校验可选长文本 (Topic.description):
// 允许空字符串, 只限制 64 KiB. 保存原始文本, 不 trim,
// Markdown 的缩进与换行有语义.
func validateOptionalText(field, raw string) (string, error) {
	if len(raw) > maxTextBytes {
		return "", apperr.Newf(apperr.CodeValidationError, "%s 最大 64 KiB", field).
			WithDetails(map[string]any{"field": field})
	}
	return raw, nil
}
