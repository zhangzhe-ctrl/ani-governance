package oss

// 本文件针对 utils.go 的 URL 拼接与下载区间设置函数做表驱动测试：
//   - JoinObjectUrl：endpoint/bucket/object 各自规范化斜杠后拼接；
//   - ReplaceEndpointHost：URL 中的 endpoint 子串替换，空入参直通；
//   - SetDownloadRange：将区间写入 minio.GetObjectOptions（经公开的 Header() 读取断言），
//     覆盖 start/end 双有、仅有 start、仅有 end、双无与 nil opts 各分支。

import (
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/require"
)

// TestJoinObjectUrl 表驱动校验 URL 拼接的斜杠规范化。
func TestJoinObjectUrl(t *testing.T) {
	tests := []struct {
		name       string
		endpoint   string
		bucketName string
		objectName string
		want       string
	}{
		{"trailing and leading slashes trimmed", "http://h/", "/b/", "/o.png", "http://h/b/o.png"},
		{"clean parts unchanged", "http://h", "b", "o.png", "http://h/b/o.png"},
		{"object leading slashes all trimmed", "http://h", "b", "///x.png", "http://h/b/x.png"},
		{"all empty yields bare slashes", "", "", "", "//"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, JoinObjectUrl(tt.endpoint, tt.bucketName, tt.objectName),
				"JoinObjectUrl(%q,%q,%q)", tt.endpoint, tt.bucketName, tt.objectName)
		})
	}
}

// TestReplaceEndpointHost 表驱动校验 endpoint 子串替换与空入参直通。
func TestReplaceEndpointHost(t *testing.T) {
	tests := []struct {
		name     string
		rawURL   string
		host     string
		endpoint string
		want     string
	}{
		{"empty url passthrough", "", "h", "e", ""},
		{"empty host passthrough", "u", "", "e", "u"},
		{"endpoint replaced by host", "http://minio:9000/a/b", "http://cdn.example", "http://minio:9000", "http://cdn.example/a/b"},
		{"no occurrence unchanged", "http://other/x", "http://cdn", "http://no-such-endpoint-host", "http://other/x"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, ReplaceEndpointHost(tt.rawURL, tt.host, tt.endpoint),
				"ReplaceEndpointHost(%q,%q,%q)", tt.rawURL, tt.host, tt.endpoint)
		})
	}
}

// TestSetDownloadRange 表驱动校验区间写入：
// (start,end) 双有 → bytes=N-M；仅 start → bytes=N-；仅 end → bytes=0-N（minio SetRange 语义）；
// 双无 → 不设置头；nil opts → 直接返回不 panic。
func TestSetDownloadRange(t *testing.T) {
	tests := []struct {
		name      string
		start     *int64
		end       *int64
		wantRange string
	}{
		{"both start and end", int64Ptr(5), int64Ptr(10), "bytes=5-10"},
		{"start only", int64Ptr(5), nil, "bytes=5-"},
		{"end only", nil, int64Ptr(-5), "bytes=-5"},
		{"end only positive", nil, int64Ptr(5), "bytes=0-5"},
		{"neither", nil, nil, ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var opts minio.GetObjectOptions
			SetDownloadRange(&opts, tt.start, tt.end)
			require.Equal(t, tt.wantRange, opts.Header().Get("Range"),
				"Range header for start=%v end=%v", tt.start, tt.end)
		})
	}
}

// TestSetDownloadRange_NilOpts nil 选项对象必须直接返回且不 panic。
func TestSetDownloadRange_NilOpts(t *testing.T) {
	t.Parallel()
	require.NotPanics(t, func() {
		SetDownloadRange(nil, nil, nil)
	})
}

// int64Ptr 构造 int64 指针的测试辅助。
func int64Ptr(v int64) *int64 { return &v }
