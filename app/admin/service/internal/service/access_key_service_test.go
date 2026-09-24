package service

import (
	accesskeyV1 "go-wind-admin/api/gen/go/access_key/service/v1"
	"go-wind-admin/pkg/localdeps/go-utils/trans"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"testing"
)

func TestAccessKeyFieldMaskJSON(t *testing.T) {
	var req accesskeyV1.UpdateAccessKeyRequest
	if err := protojson.Unmarshal([]byte(`{"key_id":1,"data":{"is_active":false,"expires_at":null,"role_id":2},"update_mask":"isActive,expiresAt,roleId"}`), &req); err != nil {
		t.Fatal(err)
	}
	if err := validateAccessKeyUpdate(&req); err != nil {
		t.Fatal(err)
	}
	if got := req.UpdateMask.Paths[0]; got != "is_active" {
		t.Fatalf("decoded %s", got)
	}
	if err := protojson.Unmarshal([]byte(`{"key_id":1,"data":{"is_active":false},"update_mask":"is_active"}`), &req); err == nil {
		t.Fatal("accepted snake_case mask value")
	}
}
func TestAccessKeyUpdateValidation(t *testing.T) {
	for _, path := range []string{"access_key", "secret_key", "tenant_id", "created_by", "id", "unknown", ""} {
		req := &accesskeyV1.UpdateAccessKeyRequest{KeyId: 1, Data: &accesskeyV1.AccessKey{}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{path}}}
		if validateAccessKeyUpdate(req) == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	if validateAccessKeyUpdate(&accesskeyV1.UpdateAccessKeyRequest{KeyId: 1, Data: &accesskeyV1.AccessKey{}}) == nil {
		t.Fatal("accepted empty mask")
	}
	req := &accesskeyV1.UpdateAccessKeyRequest{KeyId: 1, Data: &accesskeyV1.AccessKey{IsActive: trans.Ptr(false)}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"is_active", "expires_at"}}}
	if err := validateAccessKeyUpdate(req); err != nil {
		t.Fatal(err)
	}
	req.UpdateMask.Paths = []string{"role_id"}
	if validateAccessKeyUpdate(req) == nil {
		t.Fatal("accepted zero role")
	}
}
