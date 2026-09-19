package oss

import (
	"context"
	"io"

	"github.com/minio/minio-go/v7"
)

// GetObjectReader 流式读取对象内容（供签名图片代理等场景使用）。
// 调用方负责关闭返回的 reader。
func (c *MinIOClient) GetObjectReader(ctx context.Context, bucketName, objectName string) (io.ReadCloser, string, int64, error) {
	opts := minio.GetObjectOptions{}
	object, err := c.mc.GetObject(ctx, bucketName, objectName, opts)
	if err != nil {
		c.log.Errorf(ctx, "failed to get object %s/%s: %v", bucketName, objectName, err)
		return nil, "", 0, err
	}
	st, err := object.Stat()
	if err != nil {
		_ = object.Close()
		c.log.Errorf(ctx, "failed to stat object %s/%s: %v", bucketName, objectName, err)
		return nil, "", 0, err
	}
	contentType := st.ContentType
	if contentType == "" {
		contentType = DefaultContentType
	}
	return object, contentType, st.Size, nil
}
