package auth

import (
	"strings"
	"testing"
	"time"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash is not argon2id encoded: %q", hash)
	}
	if err := VerifyPassword(hash, "correct horse battery staple"); err != nil {
		t.Fatalf("VerifyPassword(correct): %v", err)
	}
	if err := VerifyPassword(hash, "wrong"); err != ErrMismatch {
		t.Fatalf("VerifyPassword(wrong) = %v, want ErrMismatch", err)
	}
}

// TestHashPasswordUniqueSalt 验证随机盐: 相同密码两次哈希结果必须不同.
func TestHashPasswordUniqueSalt(t *testing.T) {
	a, err := HashPassword("same")
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("same")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("hashes with random salt must differ")
	}
}

// TestGeneratePasswordEntropy 验证初始密码长度:
// 20 字节 Base64URL 无填充编码固定为 27 个字符.
func TestGeneratePasswordEntropy(t *testing.T) {
	pw, err := GeneratePassword()
	if err != nil {
		t.Fatal(err)
	}
	if len(pw) != 27 {
		t.Fatalf("password length = %d, want 27", len(pw))
	}
}

func TestAPIKeyPrefix(t *testing.T) {
	key, err := GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, APIKeyPrefix) {
		t.Fatalf("api key %q missing prefix", key)
	}
	if !IsAPIKey(key) {
		t.Fatal("IsAPIKey should be true")
	}
}

func TestHashTokenStable(t *testing.T) {
	if HashToken("abc") != HashToken("abc") {
		t.Fatal("HashToken must be deterministic")
	}
	if HashToken("abc") == HashToken("abd") {
		t.Fatal("HashToken collision on distinct input")
	}
}

// TestLoginAttemptsProgressiveDelay 验证渐进延迟:
// 前 5 次失败不延迟, 之后线性增长并封顶 30 秒, 成功后清零.
func TestLoginAttemptsProgressiveDelay(t *testing.T) {
	la := NewLoginAttempts()
	now := time.Now()

	for i := 0; i < loginFreeFailures; i++ {
		la.Fail(now)
		if d := la.RetryAfter(now); d != 0 {
			t.Fatalf("failure %d caused delay %v before threshold", i+1, d)
		}
	}
	la.Fail(now)
	d := la.RetryAfter(now)
	if d <= 0 || d > time.Second {
		t.Fatalf("first delayed retry = %v, want (0,1s]", d)
	}

	for i := 0; i < 100; i++ {
		la.Fail(now)
	}
	if d := la.RetryAfter(now); d > loginMaxDelay {
		t.Fatalf("delay %v exceeds cap %v", d, loginMaxDelay)
	}

	la.Reset()
	if d := la.RetryAfter(now); d != 0 {
		t.Fatalf("delay after reset = %v, want 0", d)
	}
}
