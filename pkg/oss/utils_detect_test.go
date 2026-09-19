package oss

// 本文件针对 utils.go 的 DetectFileType 做表驱动测试：
// 用构造的字节前头逐一触发魔术头判定分支（PNG/JPEG/GIF/PDF/ZIP/ftyp/ID3/MP3 帧头/RIFF-WAVE/BMP），
// 以及未命中魔数后经由 http.DetectContentType + mime.ExtensionsByType 的回退路径。
//
// 说明：文本类内容（text/plain; charset=utf-8）与 image/x-icon 的扩展名取自
// mime.ExtensionsByType 的首个结果，Windows 注册表会为这些类型注入大量额外扩展，
// 因此这些用例仅断言 MIME、不断言扩展名（见各用例 assertExt 标志）。

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

// testPNGMagic 合法的 PNG 8 字节文件头（供内容检测相关测试复用）。
var testPNGMagic = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x00}

// TestDetectFileType 表驱动校验内容嗅探：魔数分支返回固定 (MIME, 扩展名) 对；
// 未知二进制回退为 application/octet-stream + .bin；文本/ICO 仅断言 MIME。
func TestDetectFileType(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		wantMime  string
		wantExt   string
		assertExt bool // 扩展名是否可跨环境稳定断言
	}{
		// 魔术头分支（返回值为函数内固定映射，确定性断言）
		{"png magic", testPNGMagic, "image/png", ".png", true},
		{"jpeg magic", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x00, 0x00, 0x00}, "image/jpeg", ".jpg", true},
		{"gif87a magic", []byte("GIF87a    "), "image/gif", ".gif", true},
		{"gif89a magic", []byte("GIF89a    "), "image/gif", ".gif", true},
		{"pdf magic", []byte("%PDF-1.4 fake body"), "application/pdf", ".pdf", true},
		{"zip magic", append([]byte("PK\x03\x04"), 0x00, 0x00, 0x00, 0x00), "application/zip", ".zip", true},
		{"mp4 ftyp box", []byte{0x00, 0x00, 0x00, 0x00, 'f', 't', 'y', 'p', 0x00, 0x00, 0x00, 0x00}, "video/mp4", ".mp4", true},
		{"id3 tag", append([]byte("ID3"), 0x04, 0x00, 0x00), "audio/mpeg", ".mp3", true},
		{"mp3 frame header", []byte{0xFF, 0xFB, 0x00, 0x00, 0x00, 0x00}, "audio/mpeg", ".mp3", true},
		{"riff wave", []byte("RIFF\x00\x00\x00\x00WAVEfmt "), "audio/wav", ".wav", true},
		{"bmp magic", append([]byte("BM"), 0x00, 0x00, 0x00, 0x00), "image/bmp", ".bmp", true},

		// 未命中魔数：嗅探为 application/octet-stream（builtin 映射首项为 .bin，排序稳定）
		{"unknown binary falls back to octet-stream", []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}, "application/octet-stream", ".bin", true},

		// 以下用例的扩展名取决于 mime.ExtensionsByType 首项，受 Windows 注册表 MIME 映射影响，仅断言 MIME
		{"ico header mime only", []byte{0x00, 0x00, 0x01, 0x00, 0x01, 0x02, 0x03, 0x04}, "image/x-icon", "", false},
		{"plain text mime only", []byte("hello world this is plain text"), "text/plain; charset=utf-8", "", false},
		{"empty input mime only", nil, "text/plain; charset=utf-8", "", false},
		{"large input truncated to 512 bytes", bytes.Repeat([]byte("A"), 600), "text/plain; charset=utf-8", "", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mime, ext := DetectFileType(tt.data)
			require.Equal(t, tt.wantMime, mime, "DetectFileType(%q) MIME", tt.name)
			if tt.assertExt {
				require.Equal(t, tt.wantExt, ext, "DetectFileType(%q) ext", tt.name)
			}
		})
	}
}
