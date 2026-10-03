package service

import (
 "context"
 stderrors "errors"
 "strings"
 "time"

 "github.com/go-kratos/kratos/v2/errors"
 "github.com/google/uuid"
 modeldevcontractv1 "github.com/zhangzhe-ctrl/ani-modeldev-service/api/ani/modeldev/v1"
 modeldevv1 "go-wind-admin/api/gen/go/modeldev/service/v1"
 "go-wind-admin/app/admin/service/internal/data"
 "go-wind-admin/pkg/middleware/auth"
 "google.golang.org/grpc/codes"
 "google.golang.org/grpc/status"
)

func (s *ModelDevService) modelDevQueryScope(ctx context.Context, path string) (data.ModelDevResolveScope, error) {
 principal, err := auth.PrincipalFromContext(ctx)
 if err != nil || principal == nil || principal.ID == 0 { return data.ModelDevResolveScope{}, errors.Unauthorized("INVALID_LOGIN", "user login required") }
 if principal.Type != auth.SubjectUser || principal.TenantID == 0 { return data.ModelDevResolveScope{}, errors.Forbidden("FORBIDDEN", "tenant user required") }
 if s == nil || s.authorization == nil || s.tenants == nil || s.resolver == nil { return data.ModelDevResolveScope{}, modelDevQueryFailure(nil) }
 if err := s.authorization.AuthorizeQuery(ctx, principal.TenantID, principal.ID, path); err != nil { return data.ModelDevResolveScope{}, modelDevQueryFailure(err) }
 tenant, err := s.tenants.ResourceTenantID(ctx, principal.TenantID)
 if err != nil || !modelDevQueryUUID(tenant) { return data.ModelDevResolveScope{}, modelDevQueryFailure(err) }
 actor, err := principal.Actor()
 if err != nil { return data.ModelDevResolveScope{}, errors.Forbidden("FORBIDDEN", "tenant user required") }
 return data.ModelDevResolveScope{ResourceTenantID:tenant, Actor:actor}, nil
}

func (s *ModelDevService) GetExecution(ctx context.Context, in *modeldevv1.GetExecutionRequest) (*modeldevv1.GetExecutionResponse, error) {
 scope, err := s.modelDevQueryScope(ctx, data.ModelDevGetExecutionPath)
 if err != nil { return nil, err }
 if in == nil || !modelDevQueryUUID(in.ExecutionId) { return nil, modelDevQueryInvalid() }
 out, err := s.resolver.GetExecution(ctx, scope, in.ExecutionId)
 if err != nil { return nil, modelDevQueryFailure(err) }
 execution := out.Execution
 return &modeldevv1.GetExecutionResponse{Execution:&modeldevv1.ExecutionView{
  OperationId:execution.Identity.OperationId, ExecutionId:execution.Identity.ExecutionId, Name:execution.Name,
  Kind:strings.TrimPrefix(execution.Kind.String(),"EXECUTION_KIND_"), PresetId:execution.PresetId, ReleaseId:execution.ReleaseId,
  InputVersionId:execution.InputVersionId, ImageVersionId:execution.ImageVersionId,
  ComputeState:strings.TrimPrefix(execution.States.ComputeState.String(),"COMPUTE_STATE_"),
  DeliveryState:strings.TrimPrefix(execution.States.DeliveryState.String(),"DELIVERY_STATE_"),
  CloseState:strings.TrimPrefix(execution.States.CloseState.String(),"CLOSE_STATE_"),
  ResourceState:strings.TrimPrefix(execution.States.ResourceState.String(),"RESOURCE_STATE_"),
  CurrentStep:strings.TrimPrefix(execution.CurrentStep.String(),"PIPELINE_STEP_"), StopRequested:execution.StopRequested, CloseGeneration:execution.CloseGeneration,
  AcceptedAt:execution.AcceptedAt.AsTime().UTC().Format(time.RFC3339Nano), DeadlineAt:execution.DeadlineAt.AsTime().UTC().Format(time.RFC3339Nano),
  ObservedAt:execution.ObservedAt.AsTime().UTC().Format(time.RFC3339Nano),
 }}, nil
}

func (s *ModelDevService) ListExecutionArtifacts(ctx context.Context, in *modeldevv1.ListExecutionArtifactsRequest) (*modeldevv1.ListExecutionArtifactsResponse, error) {
 scope, err := s.modelDevQueryScope(ctx, data.ModelDevListArtifactsPath)
 if err != nil { return nil, err }
 if in == nil || !modelDevQueryUUID(in.ExecutionId) || in.PageSize>100 || len(in.PageToken)>2048 { return nil, modelDevQueryInvalid() }
 out, err := s.resolver.ListExecutionArtifacts(ctx, scope, in.ExecutionId, in.PageSize, in.PageToken)
 if err != nil { return nil, modelDevQueryFailure(err) }
 reply := &modeldevv1.ListExecutionArtifactsResponse{NextPageToken:out.NextPageToken}
 for _, artifact := range out.Artifacts { reply.Artifacts=append(reply.Artifacts, modelDevPublicArtifact(artifact)) }
 return reply, nil
}

func (s *ModelDevService) AuthorizeArtifactDownload(ctx context.Context, in *modeldevv1.AuthorizeArtifactDownloadRequest) (*modeldevv1.AuthorizeArtifactDownloadResponse, error) {
 scope, err := s.modelDevQueryScope(ctx, data.ModelDevDownloadArtifactPath)
 if err != nil { return nil, err }
 if in == nil || !modelDevQueryUUID(in.ArtifactId) { return nil, modelDevQueryInvalid() }
 out, err := s.resolver.AuthorizeArtifactDownload(ctx, scope, in.ArtifactId)
 if err != nil { return nil, modelDevQueryFailure(err) }
 return &modeldevv1.AuthorizeArtifactDownloadResponse{Artifact:modelDevPublicArtifact(out.Artifact), DownloadUrl:out.DownloadUrl, ExpiresAt:out.ExpiresAt.AsTime().UTC().Format(time.RFC3339Nano)}, nil
}
func modelDevPublicArtifact(in *modeldevcontractv1.ArtifactView) *modeldevv1.ArtifactView {
 return &modeldevv1.ArtifactView{ArtifactId:in.ArtifactId, ExecutionId:in.ExecutionId, Filename:in.Filename,
  Role:strings.TrimPrefix(in.Role.String(),"FILE_ROLE_"), SizeBytes:in.SizeBytes, Sha256:in.Sha256,
  DeliveryState:strings.TrimPrefix(in.DeliveryState.String(),"DELIVERY_STATE_"), VerifiedAt:in.VerifiedAt.AsTime().UTC().Format(time.RFC3339Nano)}
}
func modelDevQueryUUID(value string) bool { id, err := uuid.Parse(value); return err==nil && id!=uuid.Nil && id.String()==value }
func modelDevQueryInvalid() error { return errors.BadRequest("INVALID_MODELDEV_QUERY", "invalid modeldev query") }
func modelDevQueryFailure(err error) error {
 switch {
 case stderrors.Is(err,data.ErrModelDevAuthorizationDenied): return errors.Forbidden("FORBIDDEN","modeldev query forbidden")
 case stderrors.Is(err,context.Canceled), status.Code(err)==codes.Canceled: return errors.New(499,"MODELDEV_QUERY_CANCELED","modeldev query canceled")
 case stderrors.Is(err,context.DeadlineExceeded), status.Code(err)==codes.DeadlineExceeded: return errors.New(504,"MODELDEV_QUERY_TIMEOUT","modeldev query timed out")
 }
 switch status.Code(err) {
 case codes.NotFound, codes.PermissionDenied, codes.Unauthenticated: return errors.NotFound("RESOURCE_NOT_FOUND","resource unavailable")
 case codes.FailedPrecondition: return errors.New(412,"ARTIFACT_NOT_PUBLISHED","artifact unavailable")
 case codes.InvalidArgument: return modelDevQueryInvalid()
 default: return errors.ServiceUnavailable("MODELDEV_QUERY_UNAVAILABLE","modeldev query unavailable")
 }
}
