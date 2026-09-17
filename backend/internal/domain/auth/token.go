package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// SessionTTL 是 session 的固定有效期, 活跃使用不续期:
// 到期前一直有效, 除非用户注销或修改密码吊销全部 session.
const SessionTTL = 30 * 24 * time.Hour

// HashToken 返回 token 的 SHA-256 十六进制.
// 数据库只存哈希不存明文, 泄库也无法伪造凭据.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// LoginAttempts 是进程内登录失败计数.
// 连续失败 5 次后启用渐进延迟, 上限 30 秒, 登录成功即清零.
// 单实例部署下进程内计数已足够, 不落库.
type LoginAttempts struct {
	mu        sync.Mutex
	failures  int
	notBefore time.Time
}

const (
	loginFreeFailures = 5
	loginMaxDelay     = 30 * time.Second
)

// NewLoginAttempts 构造空计数器.
func NewLoginAttempts() *LoginAttempts { return &LoginAttempts{} }

// RetryAfter 返回距下次允许尝试的剩余时间, 0 表示可以立即尝试.
func (l *LoginAttempts) RetryAfter(now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Before(l.notBefore) {
		return l.notBefore.Sub(now)
	}
	return 0
}

// Fail 记录一次失败. 超出免费次数后按超出量线性增加延迟并封顶,
// 让暴力尝试的成本随次数上升.
func (l *LoginAttempts) Fail(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures++
	if l.failures <= loginFreeFailures {
		return
	}
	excess := l.failures - loginFreeFailures
	delay := time.Duration(excess) * time.Second
	if delay > loginMaxDelay {
		delay = loginMaxDelay
	}
	l.notBefore = now.Add(delay)
}

// Reset 在登录成功后清零计数.
func (l *LoginAttempts) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures = 0
	l.notBefore = time.Time{}
}
