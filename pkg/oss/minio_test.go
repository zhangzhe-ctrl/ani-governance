package oss

import (
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/tx7do/go-utils/trans"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	storageV1 "go-wind-admin/api/gen/go/storage/service/v1"

	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"
)

// MinIO 测试端点与凭据默认值（与既有硬编码行为保持一致）。
const (
	defaultMinioTestEndpoint  = "127.0.0.1:9000"
	defaultMinioTestAccessKey = "root"
	defaultMinioTestSecretKey = "*Abcd123456"
)

// minioTestEndpoint 返回测试用 MinIO 端点：优先读环境变量 MINIO_TEST_ENDPOINT，
// 未设置时回退默认值。本机容器把 9000 映射到 19000 时，只需
// MINIO_TEST_ENDPOINT=127.0.0.1:19000 即可让全部实测指向该容器。
func minioTestEndpoint() string {
	if v := os.Getenv("MINIO_TEST_ENDPOINT"); v != "" {
		return v
	}
	return defaultMinioTestEndpoint
}

// minioTestAccessKey 返回测试用访问密钥：环境变量 MINIO_TEST_ACCESS_KEY，缺省回退默认值。
func minioTestAccessKey() string {
	if v := os.Getenv("MINIO_TEST_ACCESS_KEY"); v != "" {
		return v
	}
	return defaultMinioTestAccessKey
}

// minioTestSecretKey 返回测试用私钥：环境变量 MINIO_TEST_SECRET_KEY，缺省回退默认值。
func minioTestSecretKey() string {
	if v := os.Getenv("MINIO_TEST_SECRET_KEY"); v != "" {
		return v
	}
	return defaultMinioTestSecretKey
}

// createTestClient 构造指向测试端点（环境变量可覆盖）的 MinIOClient。
func createTestClient() *MinIOClient {
	endpoint := minioTestEndpoint()
	return NewMinIoClient(&conf.Bootstrap{
		Oss: &conf.OSS{
			Minio: &conf.OSS_MinIO{
				Endpoint:     endpoint,
				UploadHost:   endpoint,
				DownloadHost: endpoint,
				AccessKey:    minioTestAccessKey(),
				SecretKey:    minioTestSecretKey(),
			},
		},
	}, bLogger.NopLogger())
}

// minioAvailable 探测测试端点上的 MinIO 是否可达：环境依赖测试依赖真实 MinIO 服务，
// 无环境时 skip 而非失败——环境依赖测试不应拖红常规回归。
func minioAvailable() bool {
	conn, err := net.DialTimeout("tcp", minioTestEndpoint(), 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func TestMinIoClient(t *testing.T) {
	if !minioAvailable() {
		t.Skip("local MinIO (127.0.0.1:9000) not available, skipping environment-dependent test")
	}
	cli := createTestClient()
	assert.NotNil(t, cli)

	resp, err := cli.GetUploadPresignedUrl(t.Context(), &storageV1.GetUploadPresignedUrlRequest{
		Method:        storageV1.GetUploadPresignedUrlRequest_Put,
		ContentType:   trans.String("image/jpeg"),
		BucketName:    trans.String("images"),
		FileDirectory: trans.String("20221010"),
	})
	assert.Nil(t, err)
	assert.NotNil(t, resp)
}

func TestListFile(t *testing.T) {
	cli := createTestClient()
	assert.NotNil(t, cli)

	req := &storageV1.ListOssFileRequest{
		BucketName: trans.Ptr("users"),
		Folder:     trans.Ptr("1"),
		Recursive:  trans.Ptr(true),
	}
	files, err := cli.ListFile(t.Context(), req)
	assert.Nil(t, err)
	fmt.Println(files)
}

func TestDownloadFile(t *testing.T) {
	if !minioAvailable() {
		t.Skip("local MinIO (127.0.0.1:9000) not available, skipping environment-dependent test")
	}
	cli := createTestClient()
	assert.NotNil(t, cli)

	resp, err := cli.DownloadFile(t.Context(), &storageV1.DownloadFileRequest{
		Selector: &storageV1.DownloadFileRequest_StorageObject{
			StorageObject: &storageV1.StorageObject{
				BucketName: trans.Ptr("images"),
				ObjectName: trans.Ptr("DateTimePicker.png"),
			},
		},
		PreferPresignedUrl: trans.Ptr(false),
	})
	if err != nil {
		t.Error(err)
	}
	fmt.Println(resp)
}
