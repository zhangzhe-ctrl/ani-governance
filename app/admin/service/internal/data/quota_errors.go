package data

// QUOTA-GPU-LOCAL-01 错误合同集中构造（计划 §10.3）。
// 只在 quota 相关路径使用；不修改全局错误编码器，不记录凭据。

import (
	"github.com/go-kratos/kratos/v2/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// QuotaErrInvalid 400：非法数量、未知字段、缺少 Idempotency-Key、code/type 冲突等。
func QuotaErrInvalid(msg string) error {
	return errors.BadRequest("INVALID_QUOTA_REQUEST", msg)
}

// QuotaErrIdempotencyConflict 409：相同幂等 key 携带不同有效报文。
func QuotaErrIdempotencyConflict(msg string) error {
	return errors.Conflict("IDEMPOTENCY_CONFLICT", msg)
}

// QuotaErrExceeded 409：额度不足。
func QuotaErrExceeded(msg string) error {
	return errors.Conflict("QUOTA_EXCEEDED", msg)
}

// QuotaErrNotConfigured 403：配额未配置（目录缺失/政策缺失）。
func QuotaErrNotConfigured(msg string) error {
	return errors.Forbidden("QUOTA_NOT_CONFIGURED", msg)
}

// QuotaErrAdapterUnavailable 503：适配器缺失或配置不全。
func QuotaErrAdapterUnavailable(msg string) error {
	return errors.ServiceUnavailable("QUOTA_ADAPTER_UNAVAILABLE", msg)
}

// QuotaErrAdmissionDenied 403：套餐到期/租户不可新增。
func QuotaErrAdmissionDenied(msg string) error {
	return errors.Forbidden("QUOTA_ADMISSION_DENIED", msg)
}

// QuotaErrStorageUnavailable 503：占额数据库不可用，不转发。
func QuotaErrStorageUnavailable(msg string) error {
	return errors.ServiceUnavailable("QUOTA_STORAGE_UNAVAILABLE", msg)
}

// QuotaErrHistoryPresent 409：租户存在配额账户或历史操作，拒绝物理删除。
func QuotaErrHistoryPresent(msg string) error {
	return errors.Conflict("QUOTA_HISTORY_PRESENT", msg)
}

// QuotaErrNotFound 404：跨租户资源/操作读取统一 404，不泄露对象存在性。
func QuotaErrNotFound(msg string) error {
	return errors.NotFound("QUOTA_NOT_FOUND", msg)
}

// QuotaErrInternal 500：账本内部错误（不吞错，保留上游上下文由日志记录）。
func QuotaErrInternal(msg string) error {
	return errors.InternalServer("QUOTA_INTERNAL", msg)
}

// ── 内部退额 gRPC 错误（计划 §10.3） ─────────────────────────
// 内部 gRPC listener 不走 Kratos HTTP 错误映射，直接使用 gRPC 状态码。

func releaseErrInvalid(msg string) error {
	return status.Errorf(codes.InvalidArgument, "INVALID_QUOTA_REQUEST: %s", msg)
}

func releaseErrPermissionDenied(msg string) error {
	return status.Errorf(codes.PermissionDenied, "QUOTA_RELEASE_DENIED: %s", msg)
}

func releaseErrConflict(msg string) error {
	return status.Errorf(codes.FailedPrecondition, "QUOTA_RELEASE_CONFLICT: %s", msg)
}

func releaseErrNotFound(msg string) error {
	return status.Errorf(codes.NotFound, "QUOTA_RELEASE_NOT_FOUND: %s", msg)
}

func releaseErrUnavailable(msg string) error {
	return status.Errorf(codes.Unavailable, "QUOTA_RELEASE_UNAVAILABLE: %s", msg)
}
