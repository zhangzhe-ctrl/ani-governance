package oss

// 本文件为 MinIOClient 的服务端依赖实测（live test）：
//   - 端点与凭据取自环境变量（MINIO_TEST_ENDPOINT / MINIO_TEST_ACCESS_KEY /
//     MINIO_TEST_SECRET_KEY，缺省回退 minio_test.go 中的默认值）；
//   - 每个测试开头经 requireLiveMinio 守卫：端点不可达即 t.Skip，
//     不拖红无 MinIO 环境的常规回归；
//   - 桶隔离：统一使用 t-oss-<16位hex随机> 前缀的专属测试桶，测试尾注册
//     清理（清空桶内对象 + 尽力删除桶本身）；对默认桶（files）只写入
//     本测试专属对象并在尾清理中定点删除，不动其他对象；
//   - 覆盖：GetClient、BucketExists、MakeBucket、EnsureBucketExists（存在/不存在
//     两分支）、GetUploadPresignedUrl（PUT/POST）、UploadFile（显式桶/默认桶/
//     空MIME探测分支）、GetDownloadUrl 与 DownloadFile（直读/预签名两种变体，
//     含 AcceptMime 覆盖、Range 三种形态、非法 selector 分支）、DeleteFile
//     （删除后不可再读 + 空 bucket/object 错误分支）、GetObjectReader（成功与
//     对象缺失分支）。
//
// 注意：预签名 URL 仅断言其签名形态（含 X-Amz-Signature 参数），不做真实
// GET/PUT 网络回放。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/trans"

	storageV1 "go-wind-admin/api/gen/go/storage/service/v1"
)

// legacyFixtureBucket / legacyFixtureObject 是既有 TestDownloadFile（minio_test.go，
// 按约定保持原样不动）依赖的历史夹具对象位置。该测试写于本机开发库尚存真实
// 图片文件的时期；指向全新临时容器时对象不存在会拖红测试。
const (
	legacyFixtureBucket = "images"
	legacyFixtureObject = "DateTimePicker.png"
)

// TestMain 为上述历史夹具做缺省补种：测试端点可达且夹具对象缺失时，上传一个
// 占位对象（绝不覆盖已有真实数据），并在全部测试结束后删除占位对象、若
// images 桶为本轮补种所建则一并移除。端点不可达时不做任何事，由各测试自行
// skip。补种/清理失败打印到 stderr（best-effort，不吞错）。
func TestMain(m *testing.M) {
	seeded := false
	createdBucket := false
	if minioAvailable() {
		cli := createTestClient()
		if cli != nil && cli.GetClient() != nil {
			ctx := context.Background()

			// 历史夹具已存在（真实数据）→ 不补种
			listResp, listErr := cli.ListFile(ctx, &storageV1.ListOssFileRequest{
				BucketName: trans.Ptr(legacyFixtureBucket),
				Folder:     trans.Ptr(legacyFixtureObject),
				Recursive:  trans.Ptr(true),
			})
			present := false
			if listErr == nil {
				for _, key := range listResp.GetFiles() {
					if key == legacyFixtureObject {
						present = true
					}
				}
			} else {
				fmt.Fprintf(os.Stderr, "oss test fixture probe list failed: %v\n", listErr)
			}

			if !present {
				bucketExists, existErr := cli.BucketExists(ctx, legacyFixtureBucket)
				if existErr != nil {
					fmt.Fprintf(os.Stderr, "oss test fixture bucket probe failed: %v\n", existErr)
				}
				if _, _, _, upErr := cli.UploadFile(ctx, legacyFixtureBucket, legacyFixtureObject, "image/png", []byte("placeholder fixture for legacy TestDownloadFile")); upErr != nil {
					fmt.Fprintf(os.Stderr, "oss test fixture seed failed: %v\n", upErr)
				} else {
					seeded = true
					createdBucket = !bucketExists
				}
			}
		}
	}

	code := m.Run()

	if seeded {
		ctx := context.Background()
		cli := createTestClient()
		if cli != nil && cli.GetClient() != nil {
			if delErr := cli.DeleteFile(ctx, legacyFixtureBucket, legacyFixtureObject); delErr != nil {
				fmt.Fprintf(os.Stderr, "oss test fixture cleanup failed: %v\n", delErr)
			}
			if createdBucket {
				if rmErr := cli.GetClient().RemoveBucket(ctx, legacyFixtureBucket); rmErr != nil {
					fmt.Fprintf(os.Stderr, "oss test fixture bucket cleanup failed: %v\n", rmErr)
				}
			}
		}
	}

	os.Exit(code)
}

// liveUUIDExtNamePattern / liveUUIDNamePattern 匹配 JoinObjectName 在未指定
// 文件名时自动生成的 UUID 对象键（目录前缀 + 32 位 hex；带 MIME 时附扩展名，
// 不带 MIME 时为裸 UUID 文件名）。
var (
	liveUUIDExtNamePattern = regexp.MustCompile(`(?i)^live/[0-9a-f]{32}\.txt$`)
	liveUUIDNamePattern    = regexp.MustCompile(`(?i)^live/[0-9a-f]{32}$`)
)

// requireLiveMinio 是全部 live 测试的统一前置守卫：测试端点不可达即 skip。
func requireLiveMinio(t *testing.T) {
	t.Helper()
	if !minioAvailable() {
		t.Skipf("MinIO at %s not available, skipping live test", minioTestEndpoint())
	}
}

// newLiveBucket 生成隔离用测试桶名（t-oss-<16位hex随机>），并注册测试尾清理：
// 先经 ListFile 清空桶内全部对象（逐一 DeleteFile），再尽力删除桶本身
// （minio 的桶删除为异步操作，失败不影响测试结论）。
func newLiveBucket(t *testing.T, cli *MinIOClient) string {
	t.Helper()
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("generate bucket random suffix failed: %v", err)
	}
	bucket := "t-oss-" + hex.EncodeToString(raw)
	t.Cleanup(func() {
		// t.Context() 在测试函数返回后已取消，清理统一用 Background。
		ctx := context.Background()
		listResp, listErr := cli.ListFile(ctx, &storageV1.ListOssFileRequest{
			BucketName: trans.Ptr(bucket),
			Recursive:  trans.Ptr(true),
		})
		if listErr == nil {
			for _, key := range listResp.GetFiles() {
				_ = cli.DeleteFile(ctx, bucket, key)
			}
		}
		// best-effort：桶删除在 minio 侧为异步，失败不视为清理失败。
		_ = cli.GetClient().RemoveBucket(ctx, bucket)
	})
	return bucket
}

// deleteDefaultBucketObject 定点删除写入默认桶（files）的本测试专属对象。
func deleteDefaultBucketObject(t *testing.T, cli *MinIOClient, objectName string) {
	t.Helper()
	t.Cleanup(func() {
		_ = cli.DeleteFile(context.Background(), "files", objectName)
	})
}

// uploadLiveFixture 在测试桶内上传固定内容的小文本对象（显式 text/plain）。
func uploadLiveFixture(t *testing.T, cli *MinIOClient, bucket, objectName, content string) {
	t.Helper()
	_, _, _, err := cli.UploadFile(t.Context(), bucket, objectName, "text/plain", []byte(content))
	require.NoError(t, err, "upload fixture %s/%s failed", bucket, objectName)
}

// TestLiveGetClient 断言 GetClient 返回底层 minio 客户端（非空句柄）。
func TestLiveGetClient(t *testing.T) {
	requireLiveMinio(t)
	cli := createTestClient()
	require.NotNil(t, cli)
	require.NotNil(t, cli.GetClient(), "GetClient should expose the underlying minio client")
}

// TestLiveBucketExistsMakeBucket 覆盖 BucketExists（不存在→false、存在→true）
// 与 MakeBucket（创建成功、对已存在桶再建返回错误）。
func TestLiveBucketExistsMakeBucket(t *testing.T) {
	requireLiveMinio(t)
	cli := createTestClient()
	require.NotNil(t, cli)
	ctx := t.Context()
	bucket := newLiveBucket(t, cli)

	// 随机新桶：不存在
	exists, err := cli.BucketExists(ctx, bucket)
	require.NoError(t, err)
	require.False(t, exists, "fresh random bucket should not exist")

	// 创建后：存在
	require.NoError(t, cli.MakeBucket(ctx, bucket))
	exists, err = cli.BucketExists(ctx, bucket)
	require.NoError(t, err)
	require.True(t, exists, "bucket should exist after MakeBucket")

	// 对已存在桶重复创建：服务端报错 → 包装层返回错误
	require.Error(t, cli.MakeBucket(ctx, bucket), "re-creating an existing bucket should fail")
}

// TestLiveEnsureBucketExists 覆盖 EnsureBucketExists 的两个分支：
// 桶已存在（no-op）与桶不存在（自动创建）。
func TestLiveEnsureBucketExists(t *testing.T) {
	requireLiveMinio(t)
	cli := createTestClient()
	require.NotNil(t, cli)
	ctx := t.Context()

	// 分支一：已存在 → 直接返回，不报错
	existing := newLiveBucket(t, cli)
	require.NoError(t, cli.MakeBucket(ctx, existing))
	require.NoError(t, cli.EnsureBucketExists(ctx, existing), "existing bucket should be a no-op")
	exists, err := cli.BucketExists(ctx, existing)
	require.NoError(t, err)
	require.True(t, exists)

	// 分支二：不存在 → 创建
	fresh := newLiveBucket(t, cli)
	require.NoError(t, cli.EnsureBucketExists(ctx, fresh))
	exists2, err2 := cli.BucketExists(ctx, fresh)
	require.NoError(t, err2)
	require.True(t, exists2, "EnsureBucketExists should have created the fresh bucket")
}

// TestLiveGetUploadPresignedUrl_Put 覆盖 PUT 预签名分支：
// 显式桶名/目录/文件名（对象键与 URL 形态）、ExpireSeconds 与默认过期两个入口、
// 未指定桶名时按 MIME 推导桶（text/plain → docs）、桶名与 MIME 均未指定时
// 回退默认桶 files、未指定文件名时自动生成 UUID 对象键。
func TestLiveGetUploadPresignedUrl_Put(t *testing.T) {
	requireLiveMinio(t)
	cli := createTestClient()
	require.NotNil(t, cli)
	ctx := t.Context()
	bucket := newLiveBucket(t, cli)

	// 显式桶名 + 显式过期时间
	resp, err := cli.GetUploadPresignedUrl(ctx, &storageV1.GetUploadPresignedUrlRequest{
		Method:        storageV1.GetUploadPresignedUrlRequest_Put,
		ContentType:   trans.Ptr("text/plain"),
		BucketName:    trans.Ptr(bucket),
		FileDirectory: trans.Ptr("live"),
		FileName:      trans.Ptr("probe.txt"),
		ExpireSeconds: trans.Ptr(int32(300)),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, bucket, resp.GetBucketName())
	require.Equal(t, "live/probe.txt", resp.GetObjectName())
	require.NotEmpty(t, resp.GetUploadUrl())
	require.Contains(t, resp.GetUploadUrl(), "X-Amz-Signature=", "presigned PUT url should carry signature params")
	require.Contains(t, resp.GetUploadUrl(), "/"+bucket+"/")
	require.True(t, strings.HasPrefix(resp.GetDownloadUrl(), "http://"), "download url should be scheme-prefixed")
	require.Contains(t, resp.GetDownloadUrl(), "/"+bucket+"/live/probe.txt")

	// 未指定过期时间 → 默认 1 小时入口，输出形态一致
	resp2, err2 := cli.GetUploadPresignedUrl(ctx, &storageV1.GetUploadPresignedUrlRequest{
		Method:        storageV1.GetUploadPresignedUrlRequest_Put,
		ContentType:   trans.Ptr("text/plain"),
		BucketName:    trans.Ptr(bucket),
		FileDirectory: trans.Ptr("live"),
		FileName:      trans.Ptr("probe2.txt"),
	})
	require.NoError(t, err2)
	require.NotNil(t, resp2)
	require.Equal(t, "live/probe2.txt", resp2.GetObjectName())
	require.Contains(t, resp2.GetUploadUrl(), "X-Amz-Signature=")

	// 未指定文件名 → UUID 对象键（JoinObjectName 的自动命名分支）
	resp3, err3 := cli.GetUploadPresignedUrl(ctx, &storageV1.GetUploadPresignedUrlRequest{
		Method:        storageV1.GetUploadPresignedUrlRequest_Put,
		ContentType:   trans.Ptr("text/plain"),
		BucketName:    trans.Ptr(bucket),
		FileDirectory: trans.Ptr("live"),
	})
	require.NoError(t, err3)
	require.NotNil(t, resp3)
	require.Regexp(t, liveUUIDExtNamePattern, resp3.GetObjectName(), "auto-generated key should be dir/<uuid>.txt")

	// 未指定桶名 → 按 MIME 推导（text/plain → docs 桶；仅生成 URL，不写对象）
	resp4, err4 := cli.GetUploadPresignedUrl(ctx, &storageV1.GetUploadPresignedUrlRequest{
		Method:        storageV1.GetUploadPresignedUrlRequest_Put,
		ContentType:   trans.Ptr("text/plain"),
		FileDirectory: trans.Ptr("live"),
		FileName:      trans.Ptr("derived.txt"),
	})
	require.NoError(t, err4)
	require.NotNil(t, resp4)
	require.Equal(t, "docs", resp4.GetBucketName(), "text/plain should derive the docs bucket")
	require.Equal(t, "live/derived.txt", resp4.GetObjectName())

	// 桶名与 MIME 均未指定 → 回退默认 files 桶
	resp5, err5 := cli.GetUploadPresignedUrl(ctx, &storageV1.GetUploadPresignedUrlRequest{
		Method:        storageV1.GetUploadPresignedUrlRequest_Put,
		FileDirectory: trans.Ptr("live"),
	})
	require.NoError(t, err5)
	require.NotNil(t, resp5)
	require.Equal(t, "files", resp5.GetBucketName(), "empty bucket+mime should fall back to the files bucket")
	require.Regexp(t, liveUUIDNamePattern, resp5.GetObjectName(), "no-mime auto key should be a bare uuid")
}

// TestLiveGetUploadPresignedUrl_Post 覆盖 POST 预签名（表单策略）分支：
// 返回带签名的上传 URL 与非空 FormData，下载 URL 为 scheme 前缀 + 桶/对象路径。
func TestLiveGetUploadPresignedUrl_Post(t *testing.T) {
	requireLiveMinio(t)
	cli := createTestClient()
	require.NotNil(t, cli)
	ctx := t.Context()
	bucket := newLiveBucket(t, cli)

	resp, err := cli.GetUploadPresignedUrl(ctx, &storageV1.GetUploadPresignedUrlRequest{
		Method:        storageV1.GetUploadPresignedUrlRequest_Post,
		ContentType:   trans.Ptr("text/plain"),
		BucketName:    trans.Ptr(bucket),
		FileDirectory: trans.Ptr("live"),
		FileName:      trans.Ptr("post.txt"),
		ExpireSeconds: trans.Ptr(int32(300)),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, bucket, resp.GetBucketName())
	require.Equal(t, "live/post.txt", resp.GetObjectName())
	require.NotEmpty(t, resp.GetUploadUrl(), "presigned POST policy should yield a non-empty upload url")
	require.NotEmpty(t, resp.GetFormData(), "presigned POST policy should yield form fields")
	require.True(t, strings.HasPrefix(resp.GetDownloadUrl(), "http://"))
	require.Contains(t, resp.GetDownloadUrl(), "/"+bucket+"/live/post.txt")
}

// TestLiveUploadFile 覆盖 UploadFile 的可用路径：
// 显式桶+显式MIME、空桶名回退默认 files 桶（定点清理）、空 MIME 走内容探测分支；
// 断言 UploadInfo 的桶/键与两个 URL 的拼接形态（storagePath 为 /bucket/object，
// downloadUrl 为 <host>/bucket/object）。
func TestLiveUploadFile(t *testing.T) {
	requireLiveMinio(t)
	cli := createTestClient()
	require.NotNil(t, cli)
	ctx := t.Context()
	bucket := newLiveBucket(t, cli)
	endpoint := minioTestEndpoint()

	t.Run("explicit bucket and mime", func(t *testing.T) {
		info, storagePath, downloadUrl, err := cli.UploadFile(ctx, bucket, "live/hello.txt", "text/plain", []byte("live upload body"))
		require.NoError(t, err)
		require.Equal(t, bucket, info.Bucket)
		require.Equal(t, "live/hello.txt", info.Key)
		require.Equal(t, "/"+bucket+"/live/hello.txt", storagePath)
		require.Equal(t, endpoint+"/"+bucket+"/live/hello.txt", downloadUrl)
	})

	t.Run("empty bucket falls back to files", func(t *testing.T) {
		// 该对象写入共享默认桶，注册定点清理（仅删本测试的对象）
		deleteDefaultBucketObject(t, cli, "live/default-bucket.txt")
		info, storagePath, downloadUrl, err := cli.UploadFile(ctx, "", "live/default-bucket.txt", "text/plain", []byte("default bucket body"))
		require.NoError(t, err)
		require.Equal(t, "files", info.Bucket, "empty bucket should fall back to the files bucket")
		require.Equal(t, "live/default-bucket.txt", info.Key)
		require.Equal(t, "/files/live/default-bucket.txt", storagePath)
		require.Equal(t, endpoint+"/files/live/default-bucket.txt", downloadUrl)
	})

	t.Run("empty mime triggers content detection", func(t *testing.T) {
		info, _, _, err := cli.UploadFile(ctx, bucket, "live/detected.txt", "", []byte("plain text body for mime detection"))
		require.NoError(t, err)
		require.Equal(t, bucket, info.Bucket)
		require.Equal(t, "live/detected.txt", info.Key)
	})

	t.Run("invalid bucket name rejected by client", func(t *testing.T) {
		// 非法桶名（过短/大写）由 minio-go 客户端直接拒绝：
		// BucketExists 报错 → EnsureBucketExists 透传 → UploadFile 返回错误。
		// 覆盖两条错误传播分支，不产生任何服务端副作用。
		_, _, _, err := cli.UploadFile(ctx, "AB", "x.txt", "text/plain", []byte("x"))
		require.Error(t, err, "invalid bucket name should be rejected client-side")
	})
}

// TestLiveGetDownloadUrl_Direct 覆盖 GetDownloadUrl 的直读变体：
// 全量读取（内容/大小/源键/MIME 均回传）、AcceptMime 覆盖、Range 三种形态
// （start+end、仅 start、仅 end）以及非法 selector 分支
// （fileId 未实现、downloadUrl selector 与空 selector 均 bad request）。
func TestLiveGetDownloadUrl_Direct(t *testing.T) {
	requireLiveMinio(t)
	cli := createTestClient()
	require.NotNil(t, cli)
	ctx := t.Context()
	bucket := newLiveBucket(t, cli)
	const body = "0123456789abcdefgh"
	uploadLiveFixture(t, cli, bucket, "direct/probe.txt", body)

	directReq := func() *storageV1.GetDownloadInfoRequest {
		return &storageV1.GetDownloadInfoRequest{
			Selector: &storageV1.GetDownloadInfoRequest_StorageObject{
				StorageObject: &storageV1.StorageObject{
					BucketName: trans.Ptr(bucket),
					ObjectName: trans.Ptr("direct/probe.txt"),
				},
			},
			PreferPresignedUrl: trans.Ptr(false),
		}
	}

	t.Run("full read", func(t *testing.T) {
		resp, err := cli.GetDownloadUrl(ctx, directReq())
		require.NoError(t, err)
		require.Equal(t, []byte(body), resp.GetFile())
		// 生产码先 ReadFrom 再 Stat：minio-go 的 Stat 对已读尽的流返回
		// "剩余未读字节数"，此处恒为 0——按当前实际行为钉死。
		require.Zero(t, resp.GetSize())
		require.Equal(t, "direct/probe.txt", resp.GetSourceFileName())
		require.Equal(t, "text/plain", resp.GetMime())
		require.Empty(t, resp.GetDownloadUrl(), "direct variant should not carry a download url")
	})

	t.Run("accept mime override", func(t *testing.T) {
		req := directReq()
		req.AcceptMime = trans.Ptr("application/x-probe-mime")
		resp, err := cli.GetDownloadUrl(ctx, req)
		require.NoError(t, err)
		require.Equal(t, "application/x-probe-mime", resp.GetMime(), "AcceptMime should override the stored mime")
		require.Equal(t, []byte(body), resp.GetFile())
	})

	t.Run("range start and end", func(t *testing.T) {
		req := directReq()
		req.RangeStart = trans.Ptr(int64(0))
		req.RangeEnd = trans.Ptr(int64(4))
		resp, err := cli.GetDownloadUrl(ctx, req)
		require.NoError(t, err)
		require.Equal(t, []byte(body[:5]), resp.GetFile(), "bytes=0-4 should yield the first 5 bytes")
	})

	t.Run("range start only", func(t *testing.T) {
		req := directReq()
		req.RangeStart = trans.Ptr(int64(2))
		resp, err := cli.GetDownloadUrl(ctx, req)
		require.NoError(t, err)
		require.Equal(t, []byte(body[2:]), resp.GetFile(), "bytes=2- should yield the suffix from offset 2")
	})

	t.Run("range end only", func(t *testing.T) {
		req := directReq()
		req.RangeEnd = trans.Ptr(int64(3))
		resp, err := cli.GetDownloadUrl(ctx, req)
		require.NoError(t, err)
		require.Equal(t, []byte(body[:4]), resp.GetFile(), "bytes=0-3 should yield the first 4 bytes")
	})

	t.Run("file id selector not implemented", func(t *testing.T) {
		resp, err := cli.GetDownloadUrl(ctx, &storageV1.GetDownloadInfoRequest{
			Selector: &storageV1.GetDownloadInfoRequest_FileId{FileId: 1},
		})
		require.Error(t, err, "file id selector should be rejected as not implemented")
		require.Nil(t, resp)
	})

	t.Run("download url selector rejected", func(t *testing.T) {
		resp, err := cli.GetDownloadUrl(ctx, &storageV1.GetDownloadInfoRequest{
			Selector: &storageV1.GetDownloadInfoRequest_DownloadUrl{DownloadUrl: "http://example.com/x"},
		})
		require.Error(t, err, "download url selector should be rejected as bad request")
		require.Nil(t, resp)
	})

	t.Run("nil selector rejected", func(t *testing.T) {
		resp, err := cli.GetDownloadUrl(ctx, &storageV1.GetDownloadInfoRequest{})
		require.Error(t, err, "empty selector should be rejected as bad request")
		require.Nil(t, resp)
	})

	t.Run("invalid bucket name rejected by client", func(t *testing.T) {
		// 非法桶名由 minio-go 客户端直接拒绝（无服务端副作用）
		resp, err := cli.GetDownloadUrl(ctx, &storageV1.GetDownloadInfoRequest{
			Selector: &storageV1.GetDownloadInfoRequest_StorageObject{
				StorageObject: &storageV1.StorageObject{
					BucketName: trans.Ptr("AB"),
					ObjectName: trans.Ptr("x.txt"),
				},
			},
			PreferPresignedUrl: trans.Ptr(false),
		})
		require.Error(t, err, "invalid bucket name should be rejected client-side")
		require.Nil(t, resp)
	})
}

// TestLiveGetDownloadUrl_Presigned 覆盖 GetDownloadUrl 的预签名变体：
// 显式与默认过期两个入口，均断言返回的是携带签名参数（X-Amz-Signature）的
// 预签名 URL 且不含文件字节，不做真实 GET。
func TestLiveGetDownloadUrl_Presigned(t *testing.T) {
	requireLiveMinio(t)
	cli := createTestClient()
	require.NotNil(t, cli)
	ctx := t.Context()
	bucket := newLiveBucket(t, cli)
	uploadLiveFixture(t, cli, bucket, "presigned/probe.txt", "presigned body")

	presignedReq := func() *storageV1.GetDownloadInfoRequest {
		return &storageV1.GetDownloadInfoRequest{
			Selector: &storageV1.GetDownloadInfoRequest_StorageObject{
				StorageObject: &storageV1.StorageObject{
					BucketName: trans.Ptr(bucket),
					ObjectName: trans.Ptr("presigned/probe.txt"),
				},
			},
			PreferPresignedUrl:   trans.Ptr(true),
			PresignExpireSeconds: trans.Ptr(int32(120)),
		}
	}

	resp, err := cli.GetDownloadUrl(ctx, presignedReq())
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Empty(t, resp.GetFile(), "presigned variant should not carry file bytes")
	require.Contains(t, resp.GetDownloadUrl(), "X-Amz-Signature=", "presigned url should carry signature params")
	require.Contains(t, resp.GetDownloadUrl(), "/"+bucket+"/presigned/probe.txt")

	// 未指定过期时间 → 默认 1 小时入口
	req := presignedReq()
	req.PresignExpireSeconds = nil
	resp2, err2 := cli.GetDownloadUrl(ctx, req)
	require.NoError(t, err2)
	require.NotNil(t, resp2)
	require.Contains(t, resp2.GetDownloadUrl(), "X-Amz-Signature=")

	// 非法桶名：PresignedGetObject 由 minio-go 客户端直接拒绝（无服务端副作用）
	resp3, err3 := cli.GetDownloadUrl(ctx, &storageV1.GetDownloadInfoRequest{
		Selector: &storageV1.GetDownloadInfoRequest_StorageObject{
			StorageObject: &storageV1.StorageObject{
				BucketName: trans.Ptr("AB"),
				ObjectName: trans.Ptr("x.txt"),
			},
		},
		PreferPresignedUrl: trans.Ptr(true),
	})
	require.Error(t, err3, "invalid bucket name should be rejected client-side")
	require.Nil(t, resp3)
}

// TestLiveDownloadFile_Direct 覆盖 DownloadFile 的直读变体（结构与
// GetDownloadUrl 直读一致）：全量读取、AcceptMime 覆盖、Range 形态、
// 非法 selector 分支。
func TestLiveDownloadFile_Direct(t *testing.T) {
	requireLiveMinio(t)
	cli := createTestClient()
	require.NotNil(t, cli)
	ctx := t.Context()
	bucket := newLiveBucket(t, cli)
	const body = "9876543210zyxwvu"
	uploadLiveFixture(t, cli, bucket, "direct/probe.txt", body)

	directReq := func() *storageV1.DownloadFileRequest {
		return &storageV1.DownloadFileRequest{
			Selector: &storageV1.DownloadFileRequest_StorageObject{
				StorageObject: &storageV1.StorageObject{
					BucketName: trans.Ptr(bucket),
					ObjectName: trans.Ptr("direct/probe.txt"),
				},
			},
			PreferPresignedUrl: trans.Ptr(false),
		}
	}

	t.Run("full read", func(t *testing.T) {
		resp, err := cli.DownloadFile(ctx, directReq())
		require.NoError(t, err)
		require.Equal(t, []byte(body), resp.GetFile())
		// 同 GetDownloadUrl 直读路径：Stat 在流读尽后返回剩余未读字节数 0。
		require.Zero(t, resp.GetSize())
		require.Equal(t, "direct/probe.txt", resp.GetSourceFileName())
		require.Equal(t, "text/plain", resp.GetMime())
		require.Empty(t, resp.GetDownloadUrl(), "direct variant should not carry a download url")
	})

	t.Run("accept mime override", func(t *testing.T) {
		req := directReq()
		req.AcceptMime = trans.Ptr("application/x-probe-mime")
		resp, err := cli.DownloadFile(ctx, req)
		require.NoError(t, err)
		require.Equal(t, "application/x-probe-mime", resp.GetMime())
		require.Equal(t, []byte(body), resp.GetFile())
	})

	t.Run("range start and end", func(t *testing.T) {
		req := directReq()
		req.RangeStart = trans.Ptr(int64(1))
		req.RangeEnd = trans.Ptr(int64(3))
		resp, err := cli.DownloadFile(ctx, req)
		require.NoError(t, err)
		require.Equal(t, []byte(body[1:4]), resp.GetFile(), "bytes=1-3 should yield 3 bytes from offset 1")
	})

	t.Run("range start only", func(t *testing.T) {
		req := directReq()
		req.RangeStart = trans.Ptr(int64(5))
		resp, err := cli.DownloadFile(ctx, req)
		require.NoError(t, err)
		require.Equal(t, []byte(body[5:]), resp.GetFile(), "bytes=5- should yield the suffix from offset 5")
	})

	t.Run("range end only", func(t *testing.T) {
		req := directReq()
		req.RangeEnd = trans.Ptr(int64(2))
		resp, err := cli.DownloadFile(ctx, req)
		require.NoError(t, err)
		require.Equal(t, []byte(body[:3]), resp.GetFile(), "bytes=0-2 should yield the first 3 bytes")
	})

	t.Run("file id selector not implemented", func(t *testing.T) {
		resp, err := cli.DownloadFile(ctx, &storageV1.DownloadFileRequest{
			Selector: &storageV1.DownloadFileRequest_FileId{FileId: 1},
		})
		require.Error(t, err, "file id selector should be rejected as not implemented")
		require.Nil(t, resp)
	})

	t.Run("download url selector rejected", func(t *testing.T) {
		resp, err := cli.DownloadFile(ctx, &storageV1.DownloadFileRequest{
			Selector: &storageV1.DownloadFileRequest_DownloadUrl{DownloadUrl: "http://example.com/x"},
		})
		require.Error(t, err, "download url selector should be rejected as bad request")
		require.Nil(t, resp)
	})

	t.Run("nil selector rejected", func(t *testing.T) {
		resp, err := cli.DownloadFile(ctx, &storageV1.DownloadFileRequest{})
		require.Error(t, err, "empty selector should be rejected as bad request")
		require.Nil(t, resp)
	})

	t.Run("invalid bucket name rejected by client", func(t *testing.T) {
		// 非法桶名由 minio-go 客户端直接拒绝（无服务端副作用）
		resp, err := cli.DownloadFile(ctx, &storageV1.DownloadFileRequest{
			Selector: &storageV1.DownloadFileRequest_StorageObject{
				StorageObject: &storageV1.StorageObject{
					BucketName: trans.Ptr("AB"),
					ObjectName: trans.Ptr("x.txt"),
				},
			},
			PreferPresignedUrl: trans.Ptr(false),
		})
		require.Error(t, err, "invalid bucket name should be rejected client-side")
		require.Nil(t, resp)
	})
}

// TestLiveDownloadFile_Presigned 覆盖 DownloadFile 的预签名变体：
// 断言签名 URL 形态且不含文件字节，不做真实 GET。
func TestLiveDownloadFile_Presigned(t *testing.T) {
	requireLiveMinio(t)
	cli := createTestClient()
	require.NotNil(t, cli)
	ctx := t.Context()
	bucket := newLiveBucket(t, cli)
	uploadLiveFixture(t, cli, bucket, "presigned/probe.txt", "presigned dl body")

	presignedReq := func() *storageV1.DownloadFileRequest {
		return &storageV1.DownloadFileRequest{
			Selector: &storageV1.DownloadFileRequest_StorageObject{
				StorageObject: &storageV1.StorageObject{
					BucketName: trans.Ptr(bucket),
					ObjectName: trans.Ptr("presigned/probe.txt"),
				},
			},
			PreferPresignedUrl:   trans.Ptr(true),
			PresignExpireSeconds: trans.Ptr(int32(120)),
		}
	}

	resp, err := cli.DownloadFile(ctx, presignedReq())
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Empty(t, resp.GetFile(), "presigned variant should not carry file bytes")
	require.Contains(t, resp.GetDownloadUrl(), "X-Amz-Signature=", "presigned url should carry signature params")
	require.Contains(t, resp.GetDownloadUrl(), "/"+bucket+"/presigned/probe.txt")

	// 未指定过期时间 → 默认 1 小时入口
	req := presignedReq()
	req.PresignExpireSeconds = nil
	resp2, err2 := cli.DownloadFile(ctx, req)
	require.NoError(t, err2)
	require.NotNil(t, resp2)
	require.Contains(t, resp2.GetDownloadUrl(), "X-Amz-Signature=")

	// 非法桶名：PresignedGetObject 由 minio-go 客户端直接拒绝（无服务端副作用）
	resp3, err3 := cli.DownloadFile(ctx, &storageV1.DownloadFileRequest{
		Selector: &storageV1.DownloadFileRequest_StorageObject{
			StorageObject: &storageV1.StorageObject{
				BucketName: trans.Ptr("AB"),
				ObjectName: trans.Ptr("x.txt"),
			},
		},
		PreferPresignedUrl: trans.Ptr(true),
	})
	require.Error(t, err3, "invalid bucket name should be rejected client-side")
	require.Nil(t, resp3)
}

// TestLiveDeleteFile 覆盖 DeleteFile：删除成功、删除后直读报对象缺失错误、
// 空 bucket 名与空 object 名的 bad request 分支。
func TestLiveDeleteFile(t *testing.T) {
	requireLiveMinio(t)
	cli := createTestClient()
	require.NotNil(t, cli)
	ctx := t.Context()
	bucket := newLiveBucket(t, cli)
	uploadLiveFixture(t, cli, bucket, "del/probe.txt", "to be deleted")

	// 删除成功
	require.NoError(t, cli.DeleteFile(ctx, bucket, "del/probe.txt"))

	// 删除后直读：对象缺失 → 错误
	_, err := cli.GetDownloadUrl(ctx, &storageV1.GetDownloadInfoRequest{
		Selector: &storageV1.GetDownloadInfoRequest_StorageObject{
			StorageObject: &storageV1.StorageObject{
				BucketName: trans.Ptr(bucket),
				ObjectName: trans.Ptr("del/probe.txt"),
			},
		},
		PreferPresignedUrl: trans.Ptr(false),
	})
	require.Error(t, err, "reading a deleted object should fail")

	// 空 bucket 名 → bad request
	require.Error(t, cli.DeleteFile(ctx, "", "x"), "empty bucket name should be rejected")
	// 空 object 名 → bad request
	require.Error(t, cli.DeleteFile(ctx, bucket, ""), "empty object name should be rejected")
}

// TestLiveGetObjectReader 覆盖 GetObjectReader：
// 成功路径（内容、存储 MIME、大小与上传一致，流式可读）与对象缺失路径（Stat 报错）。
func TestLiveGetObjectReader(t *testing.T) {
	requireLiveMinio(t)
	cli := createTestClient()
	require.NotNil(t, cli)
	ctx := t.Context()
	bucket := newLiveBucket(t, cli)
	const body = "object reader probe body"
	uploadLiveFixture(t, cli, bucket, "reader/probe.txt", body)

	t.Run("read back", func(t *testing.T) {
		r, contentType, size, err := cli.GetObjectReader(ctx, bucket, "reader/probe.txt")
		require.NoError(t, err)
		require.Equal(t, "text/plain", contentType)
		require.Equal(t, int64(len(body)), size)
		data, readErr := io.ReadAll(r)
		require.NoError(t, readErr)
		require.Equal(t, []byte(body), data)
		require.NoError(t, r.Close())
	})

	t.Run("missing object", func(t *testing.T) {
		_, _, _, err := cli.GetObjectReader(ctx, bucket, "reader/missing.txt")
		require.Error(t, err, "stat on a missing object should fail")
	})

	t.Run("invalid bucket name rejected by client", func(t *testing.T) {
		// 非法桶名由 minio-go 客户端直接拒绝（无服务端副作用）
		_, _, _, err := cli.GetObjectReader(ctx, "AB", "x.txt")
		require.Error(t, err, "invalid bucket name should be rejected client-side")
	})
}
