package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	accesskeyV1 "go-wind-admin/api/gen/go/access_key/service/v1"
	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	paginationV1 "go-wind-admin/pkg/localdeps/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/protobuf/types/known/emptypb"
)

type AccessKeyService struct {
	adminV1.AccessKeyServiceHTTPServer
	repo *data.AccessKeyRepo
}

func NewAccessKeyService(_ *bootstrap.Context, repo *data.AccessKeyRepo) *AccessKeyService {
	return &AccessKeyService{repo: repo}
}

// Management always requires a real user in a nonzero tenant, including direct
// service calls. A Key principal must never acquire user-management authority.
func keyManager(ctx context.Context) (uint32, uint32, error) {
	caller, err := auth.FromContext(ctx)
	if err != nil {
		return 0, 0, err
	}
	if caller.GetUserId() == 0 || caller.GetTenantId() == 0 {
		return 0, 0, adminV1.ErrorForbidden("key management requires a tenant user")
	}
	return caller.GetTenantId(), caller.GetUserId(), nil
}
func (s *AccessKeyService) List(ctx context.Context, req *paginationV1.PagingRequest) (*accesskeyV1.ListAccessKeyResponse, error) {
	if _, _, err := keyManager(ctx); err != nil {
		return nil, err
	}
	return s.repo.List(ctx, req)
}
func (s *AccessKeyService) Get(ctx context.Context, req *accesskeyV1.GetAccessKeyRequest) (*accesskeyV1.AccessKey, error) {
	if _, _, err := keyManager(ctx); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, req)
}
func (s *AccessKeyService) Create(ctx context.Context, req *accesskeyV1.CreateAccessKeyRequest) (*accesskeyV1.CreateAccessKeyResponse, error) {
	if req == nil || req.Data == nil {
		return nil, adminV1.ErrorBadRequest("data is required")
	}
	tenantID, userID, err := keyManager(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Data.GetName()) == "" || req.Data.GetRoleId() == 0 {
		return nil, adminV1.ErrorBadRequest("name and role_id are required")
	}
	if req.Data.ExpiresAt != nil {
		if err = req.Data.ExpiresAt.CheckValid(); err != nil {
			return nil, adminV1.ErrorBadRequest("invalid expires_at")
		}
	}
	ak, err := randomToken("ak-", 16)
	if err != nil {
		return nil, adminV1.ErrorInternalServerError("generate access key failed")
	}
	sk, err := randomToken("sk-", 32)
	if err != nil {
		return nil, adminV1.ErrorInternalServerError("generate secret key failed")
	}
	dto, err := s.repo.Create(ctx, req.Data, tenantID, userID, ak, sk)
	if err != nil {
		return nil, err
	}
	return &accesskeyV1.CreateAccessKeyResponse{Data: dto, SecretKey: sk}, nil
}
func validateAccessKeyUpdate(req *accesskeyV1.UpdateAccessKeyRequest) error {
	if req == nil || req.Data == nil || req.GetKeyId() == 0 || req.UpdateMask == nil || len(req.UpdateMask.Paths) == 0 {
		return adminV1.ErrorBadRequest("key_id, data and nonempty update_mask are required")
	}
	for _, p := range req.UpdateMask.Paths {
		switch p {
		case "name":
			if req.Data.Name == nil || strings.TrimSpace(req.Data.GetName()) == "" {
				return adminV1.ErrorBadRequest("name cannot be empty")
			}
		case "role_id":
			if req.Data.GetRoleId() == 0 {
				return adminV1.ErrorBadRequest("role_id cannot be empty")
			}
		case "is_active":
			if req.Data.IsActive == nil {
				return adminV1.ErrorBadRequest("is_active is required in data")
			}
		case "expires_at":
			if req.Data.ExpiresAt != nil {
				if err := req.Data.ExpiresAt.CheckValid(); err != nil {
					return adminV1.ErrorBadRequest("invalid expires_at")
				}
			}
		default:
			return adminV1.ErrorBadRequest("field cannot be updated: %s", p)
		}
	}
	return nil
}
func (s *AccessKeyService) Update(ctx context.Context, req *accesskeyV1.UpdateAccessKeyRequest) (*emptypb.Empty, error) {
	if err := validateAccessKeyUpdate(req); err != nil {
		return nil, err
	}
	tenantID, userID, err := keyManager(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.repo.Update(ctx, req, tenantID, userID); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
func (s *AccessKeyService) Delete(ctx context.Context, req *accesskeyV1.DeleteAccessKeyRequest) (*accesskeyV1.DeleteAccessKeyResponse, error) {
	if req == nil || req.GetKeyId() == 0 {
		return nil, adminV1.ErrorBadRequest("key_id is required")
	}
	tenantID, _, err := keyManager(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.repo.Delete(ctx, req.GetKeyId(), tenantID); err != nil {
		return nil, err
	}
	return &accesskeyV1.DeleteAccessKeyResponse{Status: "revoked"}, nil
}
func (s *AccessKeyService) ResetSecret(ctx context.Context, req *accesskeyV1.ResetAccessKeySecretRequest) (*accesskeyV1.CreateAccessKeyResponse, error) {
	if req == nil || req.GetKeyId() == 0 {
		return nil, adminV1.ErrorBadRequest("key_id is required")
	}
	tenantID, userID, err := keyManager(ctx)
	if err != nil {
		return nil, err
	}
	sk, err := randomToken("sk-", 32)
	if err != nil {
		return nil, adminV1.ErrorInternalServerError("generate secret key failed")
	}
	dto, err := s.repo.ResetSecret(ctx, req.GetKeyId(), tenantID, userID, sk)
	if err != nil {
		return nil, err
	}
	return &accesskeyV1.CreateAccessKeyResponse{Data: dto, SecretKey: sk}, nil
}
func randomToken(prefix string, n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random token: %w", err)
	}
	return prefix + hex.EncodeToString(b), nil
}
