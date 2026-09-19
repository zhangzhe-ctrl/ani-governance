package oss

// 本文件针对 utils.go 的对象命名纯函数做测试：
//   - GenerateFileName：四种命名策略的分发与未知类型的 UUID 回退；
//   - GenerateObjectName：目录前缀拼接与首尾斜杠清理；
//   - EnsureObjectName：空类型回退 UUID、扩展名三级推导（文件名→MIME→内容魔数）；
//   - JoinObjectName：按 MIME/调用方提供的路径与文件名拼接对象名。
//
// 确定性策略（SHA256/HMAC）断言精确值（期望值在测试内按同一算法手工计算），
// 随机性策略（UUID/时间戳）断言形状（锚定正则）。

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenerateFileName 表驱动校验命名策略分发：shape 项断言随机名的锚定正则形状，
// exact 项断言确定性哈希名的精确值。
func TestGenerateFileName(t *testing.T) {
	// 与被测函数算法一致的期望值手工构造
	shaSum := sha256.Sum256([]byte("hello"))
	shaHex := hex.EncodeToString(shaSum[:])
	mac := hmac.New(sha256.New, staticHMACSecret)
	_, _ = mac.Write(shaSum[:])
	hmacHex := hex.EncodeToString(mac.Sum(nil))

	tests := []struct {
		name     string
		content  []byte
		ext      string
		typ      GenerateFileNameType
		expect   string
		isRegexp bool // true: expect 为形状正则；false: expect 为精确值
	}{
		// 随机命名策略：形状断言
		{"uuid with ext", []byte("ignored"), ".jpg", GenerateFileNameTypeUUID, `^[0-9a-f]{32}\.jpg$`, true},
		{"uuid ext without leading dot", []byte("ignored"), "jpg", GenerateFileNameTypeUUID, `^[0-9a-f]{32}\.jpg$`, true},
		{"uuid without ext", []byte("ignored"), "", GenerateFileNameTypeUUID, `^[0-9a-f]{32}$`, true},
		{"time base without ext", []byte("ignored"), "", GenerateFileNameTypeTimeBase, `^[0-9]+_[0-9a-f]{8}$`, true},
		{"time base with ext", []byte("ignored"), ".bin", GenerateFileNameTypeTimeBase, `^[0-9]+_[0-9a-f]{8}\.bin$`, true},
		{"unknown type falls back to uuid", []byte("ignored"), ".xyz", "not-a-type", `^[0-9a-f]{32}\.xyz$`, true},

		// 确定性命名策略：精确断言
		{"content sha256 with ext", []byte("hello"), ".ext", GenerateFileNameTypeContentSHA256, shaHex + ".ext", false},
		{"content sha256 without ext", []byte("hello"), "", GenerateFileNameTypeContentSHA256, shaHex, false},
		{"hmac content with ext", []byte("hello"), ".bin", GenerateFileNameTypeHMACContent, hmacHex + ".bin", false},
		{"hmac content without ext", []byte("hello"), "", GenerateFileNameTypeHMACContent, hmacHex, false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := GenerateFileName(tt.content, tt.ext, tt.typ)
			if tt.isRegexp {
				require.Regexp(t, tt.expect, got, "GenerateFileName shape, typ=%s", tt.typ)
			} else {
				require.Equal(t, tt.expect, got, "GenerateFileName exact, typ=%s", tt.typ)
			}
		})
	}
}

// TestGenerateObjectName 表驱动校验目录前缀拼接：
// 目录被首尾去斜杠后以单斜杠连接；空目录或全斜杠目录退化为裸文件名。
func TestGenerateObjectName(t *testing.T) {
	shaSum := sha256.Sum256([]byte("hello"))
	shaHex := hex.EncodeToString(shaSum[:])

	tests := []struct {
		name     string
		dir      string
		content  []byte
		ext      string
		typ      GenerateFileNameType
		expect   string
		isRegexp bool
	}{
		{"dir prefix", "a/b", []byte("x"), ".txt", GenerateFileNameTypeUUID, `^a/b/[0-9a-f]{32}\.txt$`, true},
		{"empty dir yields bare name", "", []byte("x"), ".txt", GenerateFileNameTypeUUID, `^[0-9a-f]{32}\.txt$`, true},
		{"leading and trailing slashes trimmed", "/x/", []byte("x"), ".txt", GenerateFileNameTypeUUID, `^x/[0-9a-f]{32}\.txt$`, true},
		{"all slashes yields bare name", "///", []byte("x"), ".txt", GenerateFileNameTypeUUID, `^[0-9a-f]{32}\.txt$`, true},
		{"deterministic hash with dir", "d", []byte("hello"), ".ext", GenerateFileNameTypeContentSHA256, "d/" + shaHex + ".ext", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := GenerateObjectName(tt.dir, tt.content, tt.ext, tt.typ)
			if tt.isRegexp {
				require.Regexp(t, tt.expect, got, "GenerateObjectName shape, dir=%q", tt.dir)
			} else {
				require.Equal(t, tt.expect, got, "GenerateObjectName exact, dir=%q", tt.dir)
			}
		})
	}
}

// TestEnsureObjectName 校验空类型回退 UUID，以及扩展名三级推导：
// 文件名扩展优先，其次 MIME 映射，最后内容魔数检测。
func TestEnsureObjectName(t *testing.T) {
	t.Run("empty type defaults to uuid with name-derived ext", func(t *testing.T) {
		t.Parallel()
		got := EnsureObjectName("d", "f.bin", "", nil, "")
		require.Regexp(t, `^d/[0-9a-f]{32}\.bin$`, got)
	})

	shaSum := sha256.Sum256([]byte("hello"))
	shaHex := hex.EncodeToString(shaSum[:])
	t.Run("sha256 via content-type derived ext", func(t *testing.T) {
		t.Parallel()
		got := EnsureObjectName("", "noext", "image/png", []byte("hello"), GenerateFileNameTypeContentSHA256)
		require.Equal(t, shaHex+".png", got)
	})

	pngSum := sha256.Sum256(testPNGMagic)
	pngMac := hmac.New(sha256.New, staticHMACSecret)
	_, _ = pngMac.Write(pngSum[:])
	pngHmacHex := hex.EncodeToString(pngMac.Sum(nil))
	t.Run("hmac via content-magic derived ext", func(t *testing.T) {
		t.Parallel()
		// 内容魔数检测兜底：PNG 头 → .png
		got := EnsureObjectName("d", "noext", "application/unknown", testPNGMagic, GenerateFileNameTypeHMACContent)
		require.Equal(t, "d/"+pngHmacHex+".png", got)
	})
}

// TestJoinObjectName 表驱动校验对象名拼接：
// 未提供文件名时以 UUID + MIME 推导后缀生成；提供路径/文件名时按调用方值原样拼接。
func TestJoinObjectName(t *testing.T) {
	t.Run("uuid with mime suffix and no path", func(t *testing.T) {
		t.Parallel()
		objectName, fileName := JoinObjectName("image/png", nil, nil)
		require.Regexp(t, `^[0-9a-f]{32}\.png$`, fileName)
		require.Regexp(t, `^[0-9a-f]{32}\.png$`, objectName)
		require.Equal(t, fileName, objectName, "nil path must not alter the file name")
	})

	t.Run("video mime suffix", func(t *testing.T) {
		t.Parallel()
		objectName, fileName := JoinObjectName("video/mp4", nil, nil)
		require.Regexp(t, `^[0-9a-f]{32}\.mp4$`, fileName)
		require.Equal(t, fileName, objectName)
	})

	t.Run("empty or unknown mime yields no suffix", func(t *testing.T) {
		t.Parallel()
		for _, ct := range []string{"", "application/unknown"} {
			objectName, fileName := JoinObjectName(ct, nil, nil)
			require.Regexp(t, `^[0-9a-f]{32}$`, fileName, "contentType=%q", ct)
			require.Equal(t, fileName, objectName, "contentType=%q", ct)
		}
	})

	t.Run("provided path and file name used verbatim", func(t *testing.T) {
		t.Parallel()
		p := "p"
		n := "n.bin"
		objectName, fileName := JoinObjectName("", &p, &n)
		require.Equal(t, "n.bin", fileName)
		require.Equal(t, "p/n.bin", objectName)
	})
}
