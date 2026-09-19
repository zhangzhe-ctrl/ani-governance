package oss

// 本文件针对 utils.go 的扩展名推导纯函数做表驱动测试：
//   - ExtractFileExtension：取最后一个点之后的片段（小写化），点在开头或无点返回空；
//   - EnsureFileExtension：文件名 → MIME → 内容魔数的三级推导顺序，以及全部失败时的 "bin" 兜底。
//
// 三个分支的扩展名统一不带前导点（MIME 分支修复后与文件名/内容分支一致）。
// 内容为 nil 且 MIME 未知时不做断言：该组合会经 DetectContentType(nil) 落入
// text/plain，其后缀取 mime.ExtensionsByType 首项，在 Windows 注册表环境下不可稳定断言。

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExtractFileExtension 表驱动校验最后一个点分割与小写化规则。
func TestExtractFileExtension(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		want     string
	}{
		{"last dot wins", "a/b/c.tar.gz", "gz"},
		{"single extension", "file.txt", "txt"},
		{"uppercase lowered", "pic.PNG", "png"},
		{"multi dot", "x.y.z", "z"},
		{"no dot", "file", ""},
		{"leading dot only", ".hidden", ""},
		{"trailing dot", "a.", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, ExtractFileExtension(tt.fileName), "ExtractFileExtension(%q)", tt.fileName)
		})
	}
}

// TestEnsureFileExtension 表驱动校验三级推导优先级与兜底值。
func TestEnsureFileExtension(t *testing.T) {
	tests := []struct {
		name        string
		fileName    string
		contentType string
		content     []byte
		want        string
	}{
		// 文件名扩展优先于 MIME
		{"name wins over content type", "f.txt", "image/png", nil, "txt"},
		{"uppercase name ext lowered", "F.TXT", "", nil, "txt"},

		// MIME 分支（与文件名/内容分支一致，不带前导点）
		{"content type png dotless", "noext", "image/png", nil, "png"},
		{"content type json dotless", "noext", "application/json", nil, "json"},

		// 内容魔数分支（点被 TrimPrefix 去除）
		{"png magic via content detection", "noext", "application/unknown", testPNGMagic, "png"},

		// 全部未知 → 兜底 "bin"（未知二进制嗅探为 octet-stream，与兜底值一致，跨环境稳定）
		{"unknown binary falls back to bin", "noext", "application/unknown", []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}, "bin"},
		{"empty content type with binary content", "noext", "", []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}, "bin"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, EnsureFileExtension(tt.fileName, tt.contentType, tt.content),
				"EnsureFileExtension(%q, %q)", tt.fileName, tt.contentType)
		})
	}
}
