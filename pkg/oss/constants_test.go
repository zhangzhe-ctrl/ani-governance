package oss

// 本文件针对 constants.go 中的上传安全校验纯函数做表驱动测试：
//   - IsAllowedMimeType：MIME 白名单的精确匹配与前缀匹配语义；
//   - IsFileDirectorySafe：目录字符串的字符白名单、路径穿越与绝对路径拒绝。
//
// 断言钉住当前实现行为：前缀匹配区分大小写、精确表不做参数剥离、
// '.' 字符本身不在目录白名单内（".." 显式检查为纵深防御）等。

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIsAllowedMimeType 表驱动校验 MIME 白名单：
// 精确表（文档/压缩包/Office）命中返回 true，image/video/audio 前缀命中返回 true，
// 其余（含空串、大小写不一致、带参数、HTML/脚本/可执行等危险类型）返回 false。
func TestIsAllowedMimeType(t *testing.T) {
	tests := []struct {
		name     string
		mimeType string
		want     bool
	}{
		// 精确白名单命中
		{"exact text/plain", "text/plain", true},
		{"exact text/markdown", "text/markdown", true},
		{"exact application/json", "application/json", true},
		{"exact application/pdf", "application/pdf", true},
		{"exact application/zip", "application/zip", true},
		{"exact application/gzip", "application/gzip", true},
		{"exact application/x-gzip", "application/x-gzip", true},
		{"exact application/x-tar", "application/x-tar", true},
		{"exact application/x-7z-compressed", "application/x-7z-compressed", true},
		{"exact application/x-rar-compressed", "application/x-rar-compressed", true},
		{"exact application/msword", "application/msword", true},
		{"exact wordprocessingml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", true},
		{"exact ms-excel", "application/vnd.ms-excel", true},
		{"exact spreadsheetml", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", true},
		{"exact ms-powerpoint", "application/vnd.ms-powerpoint", true},
		{"exact presentationml", "application/vnd.openxmlformats-officedocument.presentationml.presentation", true},

		// 前缀白名单命中（输入先经 ParseMediaType 归一：剥离参数、去空白、
		// 按 RFC 大小写不敏感归小写，然后前缀匹配）
		{"prefix image png", "image/png", true},
		{"prefix image svg", "image/svg+xml", true},
		{"prefix image arbitrary subtype", "image/anything-custom", true},
		{"prefix image wildcard literal", "image/*", true},
		{"prefix image trailing space normalized", "image/png ", true},
		{"mixed case normalized", "Image/png", true},
		{"uppercase normalized", "IMAGE/PNG", true},
		{"exact text/plain params stripped", "text/plain; charset=utf-8", true},
		{"prefix video mp4", "video/mp4", true},
		{"prefix audio mpeg", "audio/mpeg", true},

		// 拒绝：空、无斜杠、畸形/通配串、白名单外类型
		{"empty", "", false},
		{"no slash image", "image", false},
		{"no slash png", "png", false},
		{"malformed bare image slash", "image/", false},
		{"text/html not whitelisted", "text/html", false},
		{"text/csv not whitelisted", "text/csv", false},
		{"application/javascript not whitelisted", "application/javascript", false},
		{"application/octet-stream not whitelisted", "application/octet-stream", false},
		{"php not whitelisted", "application/x-httpd-php", false},
		{"font not whitelisted", "font/woff", false},
		{"unknown application subtype", "application/unknown", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, IsAllowedMimeType(tt.mimeType), "IsAllowedMimeType(%q)", tt.mimeType)
		})
	}
}

// TestIsFileDirectorySafe 表驱动校验目录字符串安全：
// 合法（空、字母数字下划线连字符斜杠、尾斜杠）返回 true；
// 穿越（..）、绝对路径（前导斜杠）、白名单外字符（空格、点、符号、非 ASCII）返回 false。
func TestIsFileDirectorySafe(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		want bool
	}{
		// 合法输入
		{"empty allowed", "", true},
		{"plain letters", "abc", true},
		{"nested dirs", "a/b/c", true},
		{"underscore dash digits", "dir_1/sub-dir/012", true},
		{"trailing slash allowed", "a/b/", true},

		// 路径穿越（"." 不在字符白名单内，先被字符循环拒绝；显式 ".." 检查为纵深防御）
		{"dotdot alone", "..", false},
		{"dotdot inside", "a..b", false},
		{"parent traversal", "../etc/passwd", false},
		{"inner traversal", "a/../b", false},
		{"single dot", ".", false},
		{"dot in name", "a.b", false},
		{"percent encoded dots", "%2e%2e", false},

		// 绝对路径
		{"leading slash", "/abs", false},
		{"root slash", "/", false},

		// 白名单外字符
		{"semicolon", "a;b", false},
		{"space", "a b", false},
		{"backslash", `a\b`, false},
		{"pipe", "a|b", false},
		{"quote", "a'b", false},
		{"newline", "a\nb", false},
		{"tab", "a\tb", false},
		{"non-ascii", "café", false},
		{"nul byte", "a\x00b", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, IsFileDirectorySafe(tt.dir), "IsFileDirectorySafe(%q)", tt.dir)
		})
	}
}
