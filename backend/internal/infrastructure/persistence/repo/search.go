package repo

import "strings"

// likePattern 构造子串搜索的 LIKE 模式: 转义 LIKE 通配符后包裹百分号.
// 查询词只做 ASCII 小写折叠, 与 LOWER(col) 配合实现契约要求的
// "ASCII 大小写不敏感"; 非 ASCII 字符的折叠行为各数据库本就不同,
// 不在契约范围内, 保持原样让三种数据库行为尽量一致.
func likePattern(q string) string {
	var b strings.Builder
	b.WriteByte('%')
	for _, c := range q {
		switch c {
		case '\\', '%', '_':
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	b.WriteByte('%')
	return asciiLower(b.String())
}

// asciiLower 只折叠 ASCII 字母. UTF-8 多字节序列的高位字节都带
// 最高位标记, 不会与 ASCII 字母混淆, 逐字节处理是安全的.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
