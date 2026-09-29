package service

import (
 "context"
 "github.com/go-kratos/kratos/v2/errors"
 "github.com/go-kratos/kratos/v2/transport"
 imagev1 "github.com/zhangzhe-ctrl/ani-resource-service/api/image/v1"
 admin "go-wind-admin/api/gen/go/admin/service/v1"
 view "go-wind-admin/api/gen/go/catalog/service/v1"
 "go-wind-admin/app/admin/service/internal/data"
 "go-wind-admin/pkg/middleware/auth"
)

type ImageService struct{admin.UnimplementedImageServiceServer;client *data.ImageClient;tenants ResourceTenantResolver}
func NewImageService(client *data.ImageClient,tenants ResourceTenantResolver)*ImageService{return &ImageService{client:client,tenants:tenants}}
func(s *ImageService)trustedImageOperator(ctx context.Context)(string,string,error){
 p,err:=auth.PrincipalFromContext(ctx);if err!=nil||p==nil||p.ID==0{return "","",errors.Unauthorized("INVALID_LOGIN","login required")}
 if p.TenantID==0{return "","",errors.Forbidden("TENANT_REQUIRED","tenant identity required")}
 actor,err:=p.Actor();if err!=nil{return "","",errors.Unauthorized("INVALID_LOGIN","login required")}
 if s.client==nil||s.tenants==nil{return "","",errors.ServiceUnavailable("IMAGE_UNAVAILABLE","Image access is not configured")}
 tenant,err:=s.tenants.ResourceTenantID(ctx,p.TenantID);if err!=nil{return "","",errors.ServiceUnavailable("TENANT_MAPPING_UNAVAILABLE","tenant mapping unavailable")}
 if !imageUUID(tenant){return "","",errors.ServiceUnavailable("TENANT_MAPPING_INVALID","tenant mapping unavailable")};return tenant,actor,nil
}
func imageNoStore(ctx context.Context){if tr,ok:=transport.FromServerContext(ctx);ok{tr.ReplyHeader().Set("Cache-Control","no-store");tr.ReplyHeader().Set("Pragma","no-cache")}}
func(s *ImageService)EnsureImageSpace(ctx context.Context,r *view.EnsureImageSpaceRequest)(*view.EnsureImageSpaceResponse,error){
 if r==nil{return nil,imageBadRequest()};tenant,actor,err:=s.trustedImageOperator(ctx);if err!=nil{return nil,err}
 v,err:=s.client.EnsureImageSpace(ctx,tenant,actor,&imagev1.EnsureImageSpaceRequest{Slug:r.GetSlug(),IdempotencyKey:r.GetIdempotencyKey()});if err!=nil{return nil,mapImageError(err)}
 if err=validateImageSpace(v.GetSpace(),tenant);err!=nil{return nil,err};return &view.EnsureImageSpaceResponse{Space:wireImageSpace(v.Space)},nil
}
func(s *ImageService)GetImageSpace(ctx context.Context,r *view.GetImageSpaceRequest)(*view.GetImageSpaceResponse,error){
 if r==nil{return nil,imageBadRequest()};tenant,actor,err:=s.trustedImageOperator(ctx);if err!=nil{return nil,err}
 v,err:=s.client.GetImageSpace(ctx,tenant,actor,&imagev1.GetImageSpaceRequest{});if err!=nil{return nil,mapImageError(err)}
 if err=validateImageSpace(v.GetSpace(),tenant);err!=nil{return nil,err};return &view.GetImageSpaceResponse{Space:wireImageSpace(v.Space)},nil
}
func(s *ImageService)GetPublisherCredential(ctx context.Context,r *view.GetPublisherCredentialRequest)(*view.GetPublisherCredentialResponse,error){
 imageNoStore(ctx);if r==nil{return nil,imageBadRequest()};tenant,actor,err:=s.trustedImageOperator(ctx);if err!=nil{return nil,err}
 v,err:=s.client.GetPublisherCredential(ctx,tenant,actor,&imagev1.GetPublisherCredentialRequest{});if err!=nil{return nil,mapImageError(err)}
 if err=validateImageCredential(v.GetCredential(),tenant);err!=nil{return nil,err};return &view.GetPublisherCredentialResponse{Credential:wireImageCredential(v.Credential)},nil
}
func(s *ImageService)IssuePublisherCredential(ctx context.Context,r *view.IssuePublisherCredentialRequest)(*view.IssuePublisherCredentialResponse,error){
 imageNoStore(ctx);if r==nil{return nil,imageBadRequest()};tenant,actor,err:=s.trustedImageOperator(ctx);if err!=nil{return nil,err}
 v,err:=s.client.IssuePublisherCredential(ctx,tenant,actor,&imagev1.IssuePublisherCredentialRequest{IdempotencyKey:r.GetIdempotencyKey(),ExpectedVersion:r.GetExpectedVersion()});if err!=nil{return nil,mapImageError(err)}
 if err=validateImageDelivery(v.GetCredential(),tenant,v.GetSecret(),v.GetReplayUntil());err!=nil{return nil,err};return &view.IssuePublisherCredentialResponse{Credential:wireImageCredential(v.Credential),Secret:v.Secret,ReplayUntil:v.ReplayUntil},nil
}
func(s *ImageService)ResetPublisherCredential(ctx context.Context,r *view.ResetPublisherCredentialRequest)(*view.ResetPublisherCredentialResponse,error){
 imageNoStore(ctx);if r==nil{return nil,imageBadRequest()};tenant,actor,err:=s.trustedImageOperator(ctx);if err!=nil{return nil,err}
 v,err:=s.client.ResetPublisherCredential(ctx,tenant,actor,&imagev1.ResetPublisherCredentialRequest{IdempotencyKey:r.GetIdempotencyKey(),ExpectedVersion:r.GetExpectedVersion()});if err!=nil{return nil,mapImageError(err)}
 if err=validateImageDelivery(v.GetCredential(),tenant,v.GetSecret(),v.GetReplayUntil());err!=nil{return nil,err};return &view.ResetPublisherCredentialResponse{Credential:wireImageCredential(v.Credential),Secret:v.Secret,ReplayUntil:v.ReplayUntil},nil
}
func(s *ImageService)DisablePublisherCredential(ctx context.Context,r *view.DisablePublisherCredentialRequest)(*view.DisablePublisherCredentialResponse,error){
 imageNoStore(ctx);if r==nil{return nil,imageBadRequest()};tenant,actor,err:=s.trustedImageOperator(ctx);if err!=nil{return nil,err}
 v,err:=s.client.DisablePublisherCredential(ctx,tenant,actor,&imagev1.DisablePublisherCredentialRequest{IdempotencyKey:r.GetIdempotencyKey(),ExpectedVersion:r.GetExpectedVersion()});if err!=nil{return nil,mapImageError(err)}
 if err=validateImageCredential(v.GetCredential(),tenant);err!=nil{return nil,err};if v.Credential.State!="disabled"{return nil,imageInvalidReply()};return &view.DisablePublisherCredentialResponse{Credential:wireImageCredential(v.Credential)},nil
}
func(s *ImageService)RegisterImage(ctx context.Context,r *view.RegisterImageRequest)(*view.RegisterImageResponse,error){
 if r==nil{return nil,imageBadRequest()};tenant,actor,err:=s.trustedImageOperator(ctx);if err!=nil{return nil,err}
 p,a,err:=imageDeclarations(r.GetPurposes(),r.GetAccelerator());if err!=nil{return nil,err}
 v,err:=s.client.RegisterImage(ctx,tenant,actor,&imagev1.RegisterImageRequest{ImageReference:r.GetImageReference(),DisplayName:r.GetDisplayName(),Description:r.GetDescription(),Purposes:p,Accelerator:a,IdempotencyKey:r.GetIdempotencyKey()});if err!=nil{return nil,mapImageError(err)}
 if err=validateImageReply(v.GetImage(),tenant,imagev1.ImageScope_IMAGE_SCOPE_TENANT,"");err!=nil{return nil,err};return &view.RegisterImageResponse{Image:wireImageRegistration(v.Image)},nil
}
func(s *ImageService)GetImage(ctx context.Context,r *view.GetImageRequest)(*view.GetImageResponse,error){
 if r==nil||!imageIDPattern.MatchString(r.GetImageId()){return nil,imageBadRequest()};tenant,actor,err:=s.trustedImageOperator(ctx);if err!=nil{return nil,err}
 scope,err:=imageScope(r.GetScope());if err!=nil{return nil,err};v,err:=s.client.GetImage(ctx,tenant,actor,&imagev1.GetImageRequest{ImageId:r.GetImageId(),Scope:scope});if err!=nil{return nil,mapImageError(err)}
 if err=validateImageReply(v.GetImage(),tenant,scope,r.GetImageId());err!=nil{return nil,err};return &view.GetImageResponse{Image:wireImageRegistration(v.Image)},nil
}
func(s *ImageService)ListImages(ctx context.Context,r *view.ListImagesRequest)(*view.ListImagesResponse,error){
 if r==nil{return nil,imageBadRequest()};tenant,actor,err:=s.trustedImageOperator(ctx);if err!=nil{return nil,err}
 scope,err:=imageScope(r.GetScope());if err!=nil{return nil,err};p,a,err:=imageDeclarations(r.GetPurposes(),r.GetAccelerator());if err!=nil{return nil,err}
 v,err:=s.client.ListImages(ctx,tenant,actor,&imagev1.ListImagesRequest{Scope:scope,Search:r.GetSearch(),Purposes:p,Accelerator:a,Limit:r.GetLimit(),Cursor:r.GetCursor()});if err!=nil{return nil,mapImageError(err)}
 limit:=r.GetLimit();if limit==0{limit=20};if v==nil||len(v.GetItems())>int(limit)||len(v.GetNextCursor())>4096{return nil,imageInvalidReply()}
 out:=&view.ListImagesResponse{NextCursor:v.NextCursor,Items:make([]*view.ImageRegistration,0,len(v.Items))};seen:=map[string]bool{}
 for _,item:=range v.Items{if err=validateImageReply(item,tenant,scope,"");err!=nil{return nil,err};if item.UnregisteredAt!=nil||seen[item.ImageId]{return nil,imageInvalidReply()};seen[item.ImageId]=true;out.Items=append(out.Items,wireImageRegistration(item))};return out,nil
}
func(s *ImageService)UpdateImage(ctx context.Context,r *view.UpdateImageRequest)(*view.UpdateImageResponse,error){
 if r==nil||!imageIDPattern.MatchString(r.GetImageId()){return nil,imageBadRequest()};tenant,actor,err:=s.trustedImageOperator(ctx);if err!=nil{return nil,err}
 p,a,err:=imageDeclarations(r.GetPurposes(),r.GetAccelerator());if err!=nil{return nil,err}
 v,err:=s.client.UpdateImage(ctx,tenant,actor,&imagev1.UpdateImageRequest{ImageId:r.GetImageId(),DisplayName:r.GetDisplayName(),Description:r.GetDescription(),Purposes:p,Accelerator:a,ExpectedVersion:r.GetExpectedVersion(),IdempotencyKey:r.GetIdempotencyKey()});if err!=nil{return nil,mapImageError(err)}
 if err=validateImageReply(v.GetImage(),tenant,imagev1.ImageScope_IMAGE_SCOPE_TENANT,r.GetImageId());err!=nil{return nil,err};return &view.UpdateImageResponse{Image:wireImageRegistration(v.Image)},nil
}
func(s *ImageService)UnregisterImage(ctx context.Context,r *view.UnregisterImageRequest)(*view.UnregisterImageResponse,error){
 if r==nil||!imageIDPattern.MatchString(r.GetImageId()){return nil,imageBadRequest()};tenant,actor,err:=s.trustedImageOperator(ctx);if err!=nil{return nil,err}
 v,err:=s.client.UnregisterImage(ctx,tenant,actor,&imagev1.UnregisterImageRequest{ImageId:r.GetImageId(),ExpectedVersion:r.GetExpectedVersion(),IdempotencyKey:r.GetIdempotencyKey()});if err!=nil{return nil,mapImageError(err)}
 if err=validateImageReply(v.GetImage(),tenant,imagev1.ImageScope_IMAGE_SCOPE_TENANT,r.GetImageId());err!=nil{return nil,err};if v.Image.UnregisteredAt==nil{return nil,imageInvalidReply()};return &view.UnregisterImageResponse{Image:wireImageRegistration(v.Image)},nil
}
