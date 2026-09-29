package imagecontract

import (
 "testing"

 admin "go-wind-admin/api/gen/go/admin/service/v1"
 catalog "go-wind-admin/api/gen/go/catalog/service/v1"
)

func TestPublicImageSurface(t *testing.T) {
 file:=catalog.File_catalog_service_v1_image_proto
 forbidden:=map[string]bool{"tenant_id":true,"namespace":true,"role":true,"permissions":true,"management_secret":true,"upstream_url":true,"harbor_project_id":true}
 for i:=0;i<file.Messages().Len();i++{
  message:=file.Messages().Get(i)
  for j:=0;j<message.Fields().Len();j++{
   field:=message.Fields().Get(j)
   if forbidden[string(field.Name())]{t.Fatalf("public identity/management field %s",field.FullName())}
   if field.Name()=="secret"&&message.Name()!="IssuePublisherCredentialResponse"&&message.Name()!="ResetPublisherCredentialResponse"{t.Fatalf("unexpected secret delivery %s",field.FullName())}
  }
 }
 services:=admin.File_admin_service_v1_i_image_proto.Services()
 if services.Len()!=1||services.Get(0).Name()!="ImageService"||services.Get(0).Methods().Len()!=11{t.Fatal("unexpected public image service surface")}
 for i:=0;i<services.Get(0).Methods().Len();i++{
  name:=services.Get(0).Methods().Get(i).Name()
  if name=="GetTenantPullMaterial"||name=="ResolveImageForWorkload"{t.Fatal("runtime surface exposed")}
 }
}
