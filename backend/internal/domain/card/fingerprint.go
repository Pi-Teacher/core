// Package card 承载 Card 的纯领域规则: front 的规范化与指纹计算.
// 该包不依赖基础设施, 供应用层与后续批次的查重逻辑共用.
package card

import (
	"crypto/sha256"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// CanonicalFront 按固定顺序 NFKC -> Unicode Default Case Folding -> NFKC
// 规范化 front. 全程不 trim、不压缩空格、不删除标点或重音, 只做 Unicode
// 层面的归一, 让同一文本的不同表示得到相同字节序列.
//
// 两次 NFKC 是必要的: Case Folding 可能把单个字符展开为多个字符
// (如 U+0130 折叠为 i + U+0307), 展开结果需要再次 NFKC 才稳定.
func CanonicalFront(front string) string {
	s := norm.NFKC.String(front)
	// cases.Fold 返回的 Caser 非并发安全, 因此每次调用新建;
	// 建卡是低频操作, 分配开销可以忽略.
	s = cases.Fold().String(s)
	return norm.NFKC.String(s)
}

// FrontFingerprint 返回 canonical front 的 SHA-256 前 8 字节.
// 指纹只用于索引初筛: 64-bit 空间存在理论碰撞, 命中后必须用
// CanonicalFront 对完整文本做第二次比较.
func FrontFingerprint(front string) []byte {
	sum := sha256.Sum256([]byte(CanonicalFront(front)))
	return sum[:8]
}
