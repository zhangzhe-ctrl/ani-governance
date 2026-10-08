package service

import (
	"github.com/go-kratos/kratos/v2/errors"
	imagev1 "github.com/zhangzhe-ctrl/ani-resource-service/api/image/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"strings"
	"testing"
	"time"
)

func TestImageResponseOwnershipAndScope(t *testing.T) {
	tenant := "11111111-1111-4111-8111-111111111111"
	now := timestamppb.Now()
	digest := "sha256:" + strings.Repeat("a", 64)
	v := &imagev1.ImageRegistration{ImageId: "img_" + strings.Repeat("a", 32), TenantId: tenant, Scope: 1, SpaceId: "22222222-2222-4222-8222-222222222222", DisplayName: "test", Repository: "test", Digest: digest, SourceReference: "registry.example.test/t-test/test:v1", ResolvedReference: "registry.example.test/t-test/test@" + digest, Platforms: []*imagev1.ImagePlatform{{Os: "linux", Architecture: "amd64"}}, Purposes: []imagev1.ImagePurpose{1}, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := validateImageReply(v, tenant, 1, v.ImageId); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*imagev1.ImageRegistration){"tenant": func(v *imagev1.ImageRegistration) { v.TenantId = "33333333-3333-4333-8333-333333333333" }, "scope": func(v *imagev1.ImageRegistration) { v.Scope = 2 }, "tag in resolved": func(v *imagev1.ImageRegistration) { v.ResolvedReference = v.SourceReference }, "digest": func(v *imagev1.ImageRegistration) { v.Digest = "sha256:bad" }, "missing platform": func(v *imagev1.ImageRegistration) { v.Platforms = nil }, "unknown purpose": func(v *imagev1.ImageRegistration) { v.Purposes = []imagev1.ImagePurpose{99} }, "unknown accelerator": func(v *imagev1.ImageRegistration) { v.Accelerator = 99 }} {
		t.Run(name, func(t *testing.T) {
			bad := proto.Clone(v).(*imagev1.ImageRegistration)
			mutate(bad)
			if errors.Code(validateImageReply(bad, tenant, 1, v.ImageId)) != 503 {
				t.Fatal("invalid reply exposed")
			}
		})
	}
	v.Scope = 2
	v.TenantId = ""
	if err := validateImageReply(v, tenant, 2, v.ImageId); err != nil {
		t.Fatal("platform read denied", err)
	}
	v.TenantId = tenant
	if err := validateImageReply(v, tenant, 2, v.ImageId); err == nil {
		t.Fatal("tenant-bound platform reply accepted")
	}
	c := &imagev1.PublisherCredential{TenantId: tenant, SpaceId: v.SpaceId, State: "active", Version: 1, Generation: 1, Username: "actual-provider-user", UpdatedAt: now}
	if err := validateImageDelivery(c, tenant, "secret", timestamppb.New(time.Now().Add(10*time.Minute))); err != nil {
		t.Fatal(err)
	}
	if err := validateImageDelivery(c, tenant, "secret", timestamppb.New(time.Now().Add(time.Hour))); err == nil {
		t.Fatal("unbounded replay accepted")
	}
	s, _ := status.New(codes.Aborted, "sensitive-provider-message").WithDetails(&errdetails.ErrorInfo{Domain: "image.ani.io", Reason: "VERSION_CONFLICT"})
	e := mapImageError(s.Err())
	if errors.Code(e) != 409 || errors.Reason(e) != "VERSION_CONFLICT" || strings.Contains(e.Error(), "sensitive") {
		t.Fatal("Image error contract lost")
	}
	s, _ = status.New(codes.FailedPrecondition, "sensitive-provider-message").WithDetails(&errdetails.ErrorInfo{Domain: "image.ani.io", Reason: "CREDENTIAL_NOT_ISSUED"})
	e = mapImageError(s.Err())
	if errors.Code(e) != 409 || errors.Reason(e) != "CREDENTIAL_NOT_ISSUED" || strings.Contains(e.Error(), "sensitive") {
		t.Fatal("not-issued publisher error contract lost")
	}
}
