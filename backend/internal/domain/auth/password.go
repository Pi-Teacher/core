// Package auth 包含密码哈希, token 生成与凭据格式, 不依赖任何基础设施.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id 成本参数. 参数随哈希字符串自描述, 以后调整成本不需要迁移旧数据.
const (
	argonTime    uint32 = 1
	argonMemory  uint32 = 64 * 1024 // 64 MiB
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

// InitialPasswordBytes 是自动生成的初始/重置密码的随机字节数.
const InitialPasswordBytes = 20

// SessionTokenBytes 是 session 与 CSRF token 的随机字节数.
const SessionTokenBytes = 32

// APIKeyBytes 是 API Key 去掉前缀后的随机字节数.
const APIKeyBytes = 32

// APIKeyPrefix 标识 Pi Teacher 的 API Key.
const APIKeyPrefix = "ptk_"

// ErrMismatch 表示密码不匹配.
var ErrMismatch = errors.New("auth: password mismatch")

// HashPassword 派生 Argon2id 哈希并返回自描述编码,
// 版本, 成本参数, 盐和哈希都在编码串里, 校验时无需外部状态.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	enc := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
	return enc, nil
}

// VerifyPassword 校验密码是否匹配编码哈希.
// 不匹配返回 ErrMismatch; 只有哈希格式损坏才返回其他错误,
// 调用方据此区分"密码错"和"存储损坏".
func VerifyPassword(encoded, password string) error {
	memory, iterations, threads, salt, want, err := decodeHash(encoded)
	if err != nil {
		return err
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrMismatch
	}
	return nil
}

// decodeHash 解析自描述编码中的成本参数, 盐和哈希.
func decodeHash(encoded string) (memory, iterations uint32, threads uint8, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return 0, 0, 0, nil, nil, errors.New("auth: unrecognized hash format")
	}
	if _, err = fmt.Sscanf(parts[2], "v=%d", new(int)); err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("auth: bad version: %w", err)
	}
	if _, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("auth: bad params: %w", err)
	}
	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("auth: bad salt: %w", err)
	}
	key, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("auth: bad key: %w", err)
	}
	return memory, iterations, threads, salt, key, nil
}

// GeneratePassword 生成 URL 安全的随机初始/重置密码,
// Base64URL 无歧义字符, 便于手工抄录.
func GeneratePassword() (string, error) {
	b := make([]byte, InitialPasswordBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GenerateToken 生成 n 个随机字节的 URL 安全 base64 编码.
func GenerateToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GenerateAPIKey 生成带 ptk_ 前缀的明文 API Key.
func GenerateAPIKey() (string, error) {
	token, err := GenerateToken(APIKeyBytes)
	if err != nil {
		return "", err
	}
	return APIKeyPrefix + token, nil
}

// IsAPIKey 判断凭据是否形如 API Key.
func IsAPIKey(s string) bool { return strings.HasPrefix(s, APIKeyPrefix) }
