package card

import (
	"bytes"
	"testing"
)

// TestFrontFingerprintStable 验证相同输入得到相同指纹, 且长度固定 8 字节.
func TestFrontFingerprintStable(t *testing.T) {
	a := FrontFingerprint("Go slice 的底层结构是什么?")
	b := FrontFingerprint("Go slice 的底层结构是什么?")
	if !bytes.Equal(a, b) {
		t.Fatalf("same input produced different fingerprints")
	}
	if len(a) != 8 {
		t.Fatalf("fingerprint length = %d, want 8", len(a))
	}
}

// TestFrontFingerprintNormalization 验证规范化链路:
// NFKC 归一 (组合字符与兼容形式), 大小写折叠, 以及折叠后的再次 NFKC.
func TestFrontFingerprintNormalization(t *testing.T) {
	cases := []struct {
		name string
		a, b string
	}{
		// 大小写折叠: ASCII 与 Unicode 均归一.
		{"ascii case", "HELLO go", "hello GO"},
		// NFKC: 组合字符 e + U+0301 归一为预组合的 U+00E9.
		{"combining accent", "cafe\u0301", "caf\u00e9"},
		// NFKC: 连字 fi (U+FB01) 归一为两个字符 fi.
		{"ligature", "de\uFB01ne", "define"},
		// NFKC: 全角字母归一为半角.
		{"fullwidth", "Ｇｏ", "Go"},
		// Case Folding 展开后再 NFKC: U+0130 折叠为 i + U+0307,
		// 与手工组合序列指纹一致.
		{"fold then nfkc", "\u0130", "i\u0307"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fa, fb := FrontFingerprint(tc.a), FrontFingerprint(tc.b)
			if !bytes.Equal(fa, fb) {
				t.Fatalf("fingerprint mismatch for %q vs %q", tc.a, tc.b)
			}
			if CanonicalFront(tc.a) != CanonicalFront(tc.b) {
				t.Fatalf("canonical mismatch for %q vs %q", tc.a, tc.b)
			}
		})
	}
}

// TestFrontFingerprintPreservesText 验证规范化不做的事:
// 不 trim、不压缩空格、不删标点, 这些差异必须保留为不同指纹.
func TestFrontFingerprintPreservesText(t *testing.T) {
	cases := []struct {
		name string
		a, b string
	}{
		{"leading space", " hello", "hello"},
		{"inner spaces", "a  b", "a b"},
		{"punctuation", "go?", "go"},
		{"accent kept", "resume", "résumé"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if bytes.Equal(FrontFingerprint(tc.a), FrontFingerprint(tc.b)) {
				t.Fatalf("expected different fingerprints for %q vs %q", tc.a, tc.b)
			}
		})
	}
}
