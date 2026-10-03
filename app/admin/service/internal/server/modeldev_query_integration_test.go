//go:build modeldev_pg && modeldev_contract

package server

import (
 "context"
 "crypto/rand"
 "crypto/sha256"
 "crypto/tls"
 "crypto/x509"
 "encoding/hex"
 "encoding/json"
 "io"
 "net"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "testing"
 "time"

 khttp "github.com/go-kratos/kratos/v2/transport/http"
 "github.com/google/uuid"
 "github.com/redis/go-redis/v9"
 "github.com/stretchr/testify/require"
 authv1 "go-wind-admin/api/gen/go/authentication/service/v1"
 identityv1 "go-wind-admin/api/gen/go/identity/service/v1"
 modeldevv1 "go-wind-admin/api/gen/go/modeldev/service/v1"
 "go-wind-admin/app/admin/service/internal/data"
 "go-wind-admin/app/admin/service/internal/data/ent/api"
 "go-wind-admin/app/admin/service/internal/data/ent/permission"
 "go-wind-admin/app/admin/service/internal/data/ent/planmodule"
 "go-wind-admin/app/admin/service/internal/data/ent/role"
 "go-wind-admin/app/admin/service/internal/data/ent/rolepermission"
 "go-wind-admin/app/admin/service/internal/data/ent/tenant"
 "go-wind-admin/app/admin/service/internal/data/ent/user"
 "go-wind-admin/app/admin/service/internal/data/ent/userrole"
 "go-wind-admin/app/admin/service/internal/service"
 "go-wind-admin/app/admin/service/tests/testutil"
 "go-wind-admin/pkg/authorizer"
 appViewer "go-wind-admin/pkg/entgo/viewer"
 "go-wind-admin/pkg/localdeps/go-utils/trans"
 authzMiddleware "go-wind-admin/pkg/localdeps/kratos-authz/middleware"
 conf "go-wind-admin/pkg/localdeps/kratos-bootstrap/api/gen/go/conf/v1"
 "go-wind-admin/pkg/middleware/auth"
 "google.golang.org/protobuf/encoding/protojson"
)

type modelDevQueryFixture struct {
 Schema string `json:"schema"`
 Address string `json:"address"`
 TLS struct { CAFile string `json:"ca_file"`; CertFile string `json:"cert_file"`; KeyFile string `json:"key_file"` } `json:"tls"`
 ResourceTenantID string `json:"resource_tenant_id"`
 ExecutionID string `json:"execution_id"`
 Artifact struct { ID string `json:"artifact_id"`; Filename string `json:"filename"`; Size int64 `json:"size_bytes"`; SHA256 string `json:"sha256"` } `json:"artifact"`
 StorageCAFile string `json:"storage_ca_file"`
}

func readModelDevQueryFixture(t *testing.T) modelDevQueryFixture {
 t.Helper()
 file:=os.Getenv("ANI_MODELDEV_QUERY_HANDSHAKE")
 if !filepath.IsAbs(file) || filepath.Base(file)!="handshake.json" { t.Fatal("CPU09_QUERY_PREFLIGHT: explicit private handshake required") }
 info,err:=os.Lstat(filepath.Dir(file)); if err!=nil || !info.IsDir() || info.Mode().Perm()!=0700 { t.Fatal("CPU09_QUERY_PREFLIGHT: private provider directory required") }
 t.Cleanup(func(){ stop,err:=os.OpenFile(filepath.Join(filepath.Dir(file),"stop"),os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600); require.NoError(t,err); if err==nil { require.NoError(t,stop.Close()) } })
 info,err=os.Lstat(file); if err!=nil || !info.Mode().IsRegular() || info.Mode().Perm()!=0600 || info.Size()>16384 { t.Fatal("CPU09_QUERY_PREFLIGHT: bounded private handshake required") }
 raw,err:=os.ReadFile(file); require.NoError(t,err)
 decoder:=json.NewDecoder(strings.NewReader(string(raw))); decoder.DisallowUnknownFields()
 var fixture modelDevQueryFixture
 require.NoError(t,decoder.Decode(&fixture)); require.Equal(t,io.EOF,decoder.Decode(new(any)))
 host,port,err:=net.SplitHostPort(fixture.Address); n,portErr:=strconv.ParseUint(port,10,16)
 if err!=nil || host!="127.0.0.1" || portErr!=nil || n==0 || fixture.Schema!="ani.cpu-p01.governance-query-fixture.v1" { t.Fatal("CPU09_QUERY_PREFLIGHT: invalid provider connection") }
 for _,path:=range []string{fixture.TLS.CAFile,fixture.TLS.CertFile,fixture.TLS.KeyFile,fixture.StorageCAFile} { if !filepath.IsAbs(path) || filepath.Dir(path)!=filepath.Dir(file) { t.Fatal("CPU09_QUERY_PREFLIGHT: private TLS file references required") } }
 for _,id:=range []string{fixture.ResourceTenantID,fixture.ExecutionID,fixture.Artifact.ID} { parsed,e:=uuid.Parse(id); if e!=nil || parsed==uuid.Nil || parsed.String()!=id { t.Fatal("CPU09_QUERY_PREFLIGHT: invalid immutable identity") } }
 return fixture
}

// This test calls the real ModelDev query service on its real PG publication.
// Only the KFP/Kubernetes/S3 boundaries in the provider remain explicit fixtures.
func TestModelDevQueryCurrentAuthorizationDownloadsPublishedArtifacts(t *testing.T) {
 provider:=readModelDevQueryFixture(t)
 ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second); t.Cleanup(cancel)
 sys:=appViewer.NewSystemViewerContext(ctx)
 runtime,writer:=prepareModelDevHTTPDatabase(t,ctx)
 cleanup:=func(remove func(context.Context)error){ t.Cleanup(func(){ c,stop:=context.WithTimeout(appViewer.NewSystemViewerContext(context.Background()),5*time.Second); defer stop(); require.NoError(t,remove(c)) }) }
 suffix:=uuid.NewString()
 plan,err:=writer.Client().Plan.Create().SetName("cpu-query-"+suffix).Save(sys); require.NoError(t,err)
 cleanup(func(c context.Context)error{return writer.Client().Plan.DeleteOneID(plan.ID).Exec(c)})
 module,err:=writer.Client().PlanModule.Create().SetPlanID(plan.ID).SetModule(planmodule.ModuleModel).Save(sys); require.NoError(t,err)
 cleanup(func(c context.Context)error{return writer.Client().PlanModule.DeleteOneID(module.ID).Exec(c)})
 paths:=[]string{data.ModelDevGetExecutionPath,data.ModelDevListArtifactsPath,data.ModelDevDownloadArtifactPath}
 var apiIDs []uint32
 for _,path:=range paths {
  row,e:=writer.Client().Api.Create().SetModule("ModelDevService").SetScope(api.ScopeAdmin).SetPath(path).SetMethod("GET").SetBusinessModule(api.BusinessModuleModel).SetStatus(api.StatusOn).Save(sys); require.NoError(t,e)
  apiIDs=append(apiIDs,row.ID); cleanup(func(c context.Context)error{return writer.Client().Api.DeleteOneID(row.ID).Exec(c)})
 }
 type identity struct { tenant,user,role,permission uint32; roleCode string }
 makeIdentity:=func(resourceTenant string)identity{
  id:=uuid.NewString()
  owner,e:=writer.Client().Tenant.Create().SetName("query fixture").SetCode("cpu-query-"+id).SetResourceTenantID(resourceTenant).SetStatus(tenant.StatusOn).SetPlanID(plan.ID).Save(sys); require.NoError(t,e)
  cleanup(func(c context.Context)error{return writer.Client().Tenant.DeleteOneID(owner.ID).Exec(c)})
  operator,e:=writer.Client().User.Create().SetTenantID(owner.ID).SetUsername("cpu-query-"+id).SetStatus(user.StatusNormal).Save(sys); require.NoError(t,e)
  cleanup(func(c context.Context)error{return writer.Client().User.DeleteOneID(operator.ID).Exec(c)})
  currentRole,e:=writer.Client().Role.Create().SetTenantID(owner.ID).SetName("query fixture").SetCode("tenant:cpu-query:"+id).SetType(role.TypeTenant).SetStatus(role.StatusOn).SetDataScope(role.DataScopeAll).Save(sys); require.NoError(t,e)
  cleanup(func(c context.Context)error{return writer.Client().Role.DeleteOneID(currentRole.ID).Exec(c)})
  membership,e:=writer.Client().UserRole.Create().SetTenantID(owner.ID).SetUserID(operator.ID).SetRoleID(currentRole.ID).SetStatus(userrole.StatusActive).Save(sys); require.NoError(t,e)
  cleanup(func(c context.Context)error{_,e:=writer.Client().UserRole.Delete().Where(userrole.IDEQ(membership.ID)).Exec(c);return e})
  grantPermission,e:=writer.Client().Permission.Create().SetName("query fixture").SetCode("cpu.query."+id).SetStatus(permission.StatusOn).Save(sys); require.NoError(t,e)
  cleanup(func(c context.Context)error{return writer.Client().Permission.DeleteOneID(grantPermission.ID).Exec(c)})
  for _,apiID:=range apiIDs { link,e:=writer.Client().PermissionApi.Create().SetPermissionID(grantPermission.ID).SetAPIID(apiID).Save(sys);require.NoError(t,e);cleanup(func(c context.Context)error{return writer.Client().PermissionApi.DeleteOneID(link.ID).Exec(c)}) }
  grant,e:=writer.Client().RolePermission.Create().SetTenantID(owner.ID).SetRoleID(currentRole.ID).SetPermissionID(grantPermission.ID).SetStatus(rolepermission.StatusOn).SetEffect(rolepermission.EffectAllow).Save(sys); require.NoError(t,e)
  cleanup(func(c context.Context)error{return writer.Client().RolePermission.DeleteOneID(grant.ID).Exec(c)})
  return identity{owner.ID,operator.ID,currentRole.ID,grantPermission.ID,*currentRole.Code}
 }
 owner,other:=makeIdentity(provider.ResourceTenantID),makeIdentity(uuid.NewString())
 unauthorized,err:=writer.Client().User.Create().SetTenantID(owner.tenant).SetUsername("cpu-query-denied-"+suffix).SetStatus(user.StatusNormal).Save(sys);require.NoError(t,err)
 cleanup(func(c context.Context)error{return writer.Client().User.DeleteOneID(unauthorized.ID).Exec(c)})
 key:=make([]byte,32); _,err=rand.Read(key);require.NoError(t,err)
 for _,name:=range []string{"GWA_AUTH_JWT_PRIVATE_KEY","GWA_AUTH_JWT_PUBLIC_KEY","GWA_AUTH_JWT_KEY"}{t.Setenv(name,"")}
 bctx:=testutil.NewBootstrapContext(&conf.Bootstrap{Authz:&conf.Authorization{Type:"casbin"},Authn:&conf.Authentication{Jwt:&conf.Authentication_Jwt{Method:"HS256",Key:hex.EncodeToString(key)}}})
 address:=os.Getenv("ANI_MODELDEV_REDIS_ADDR"); host,port,err:=net.SplitHostPort(address);n,e:=strconv.ParseUint(port,10,16);if err!=nil||host!="127.0.0.1"||e!=nil||n==0{t.Fatal("CPU09_QUERY_PREFLIGHT: task loopback Redis required")}
 rdb:=redis.NewClient(&redis.Options{Addr:address});t.Cleanup(func(){require.NoError(t,rdb.Close())});require.NoError(t,rdb.Ping(ctx).Err())
 cache:=data.NewUserTokenCache(bctx,rdb); authenticator:=data.NewAuthenticator(bctx,cache); checker:=data.NewTokenChecker(bctx,authenticator,authv1.ClientType_admin)
 tenantChecker:=data.NewTenantAccessCheckerImpl(bctx,runtime)
 permissions:=data.NewPermissionRepo(bctx,runtime,data.NewPermissionApiRepo(bctx,runtime),data.NewPermissionMenuRepo(bctx,runtime))
 roles:=data.NewRoleRepo(bctx,runtime,data.NewRolePermissionRepo(bctx,runtime),data.NewRoleOrgUnitRepo(bctx,runtime),permissions,data.NewRoleMetadataRepo(bctx,runtime),data.NewRoleFieldPermissionRepo(bctx,runtime))
 policy:=authorizer.NewAuthorizer(bctx,data.NewAuthorizerProvider(bctx,roles,data.NewApiRepo(bctx,runtime)));require.Equal(t,"casbin",policy.Engine().Name());require.NoError(t,policy.ResetPolicies(ctx))
 issue:=func(who identity)string{payload:=&authv1.UserTokenPayload{UserId:who.user,TenantId:trans.Ptr(who.tenant),Roles:[]string{who.roleCode},DataScopes:[]identityv1.DataScope{identityv1.DataScope_ALL}};token,_,e:=authenticator.CreateUserToken(ctx,authv1.ClientType_admin,payload);require.NoError(t,e);cleanup(func(c context.Context)error{return cache.RevokeTokenByJti(c,authv1.ClientType_admin,who.user,payload.GetJti())});return token}
 token,otherToken:=issue(owner),issue(other)
 denied:=owner;denied.user=unauthorized.ID;deniedToken:=issue(denied) // stale/forged role claim cannot replace current DB membership.
 resolver,closeResolver,err:=data.NewModelDevClient(data.ModelDevClientConfig{Address:provider.Address,CAFile:provider.TLS.CAFile,CertFile:provider.TLS.CertFile,KeyFile:provider.TLS.KeyFile,Timeout:5*time.Second});require.NoError(t,err);t.Cleanup(closeResolver)
 bff:=service.NewModelDevService(data.NewModelDevAuthorizationRepo(runtime),data.NewTenantRepo(bctx,runtime),nil,nil,resolver)
 server:=khttp.NewServer(khttp.Middleware(auth.CredentialHeaders(),auth.Server(auth.WithAccessTokenChecker(checker),auth.WithTenantAccessChecker(tenantChecker),auth.WithInjectMetadata(false),auth.WithInjectEnt(true)),authzMiddleware.Server(policy.Engine())))
 registerModelDevHTTP(server,bff);web:=httptest.NewServer(server);t.Cleanup(web.Close)
 get:=func(path,accessToken string)(int,[]byte){t.Helper();request,e:=http.NewRequestWithContext(ctx,"GET",web.URL+path,nil);require.NoError(t,e);if accessToken!=""{request.Header.Set("Authorization","Bearer "+accessToken)};request.Header.Set("x-ani-tenant-id",provider.ResourceTenantID);request.Header.Set("x-ani-actor","governance:user:42");request.Header.Set("x-ani-authorized-method","forged");request.Header.Set("x-ani-data-scope","tenant-all");response,e:=web.Client().Do(request);require.NoError(t,e);defer response.Body.Close();body,e:=io.ReadAll(io.LimitReader(response.Body,65537));require.NoError(t,e);require.LessOrEqual(t,len(body),65536);require.Equal(t,"no-store",response.Header.Get("Cache-Control"));return response.StatusCode,body}
 detailPath:="/admin/v1/modeldev/executions/"+provider.ExecutionID
 artifactsPath:=detailPath+"/artifacts"
 downloadPath:="/admin/v1/modeldev/artifacts/"+provider.Artifact.ID+"/content"
 for _,path:=range []string{detailPath,artifactsPath,downloadPath}{
  code,body:=get(path,"");require.Equal(t,http.StatusUnauthorized,code);require.NotContains(t,string(body),provider.Artifact.SHA256)
  code,body=get(path,deniedToken);require.Equal(t,http.StatusForbidden,code);require.NotContains(t,string(body),provider.Artifact.SHA256)
  code,body=get(path,otherToken);require.Equal(t,http.StatusNotFound,code);require.NotContains(t,string(body),provider.Artifact.SHA256)
 }
 code,body:=get(detailPath,token);require.Equal(t,http.StatusOK,code)
 var detail modeldevv1.GetExecutionResponse;require.NoError(t,protojson.Unmarshal(body,&detail));require.Equal(t,provider.ExecutionID,detail.GetExecution().GetExecutionId());require.Equal(t,"PUBLISHED",detail.GetExecution().GetDeliveryState());require.Equal(t,"CLOSED",detail.GetExecution().GetCloseState())
 require.NotContains(t,string(body),"download_url");require.NotContains(t,string(body),"bucket");require.NotContains(t,string(body),"token")
 code,body=get(artifactsPath,token);require.Equal(t,http.StatusOK,code)
 var listed modeldevv1.ListExecutionArtifactsResponse;require.NoError(t,protojson.Unmarshal(body,&listed));require.Empty(t,listed.NextPageToken);require.GreaterOrEqual(t,len(listed.Artifacts),2)
 roots:=x509.NewCertPool();ca,err:=os.ReadFile(provider.StorageCAFile);require.NoError(t,err);require.True(t,roots.AppendCertsFromPEM(ca))
 transport:=&http.Transport{TLSClientConfig:&tls.Config{MinVersion:tls.VersionTLS12,RootCAs:roots}};t.Cleanup(transport.CloseIdleConnections)
 storage:=&http.Client{Transport:transport,Timeout:15*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}
 output:=os.Getenv("ANI_MODELDEV_QUERY_OUTPUT_DIR");if !filepath.IsAbs(output){t.Fatal("CPU09_QUERY_PREFLIGHT: independent download directory required")};require.NoError(t,os.Mkdir(output,0700))
 found:=map[string]bool{}
 for _,artifact:=range listed.Artifacts{
  code,body=get("/admin/v1/modeldev/artifacts/"+artifact.ArtifactId+"/content",token);require.Equal(t,http.StatusOK,code)
  var grant modeldevv1.AuthorizeArtifactDownloadResponse;err=protojson.Unmarshal(body,&grant);if err!=nil{t.Fatal("invalid download grant response")};require.Equal(t,artifact.ArtifactId,grant.GetArtifact().GetArtifactId())
  response,e:=storage.Get(grant.DownloadUrl);if e!=nil{t.Fatal("authorized HTTPS object retrieval failed")};if response.StatusCode!=http.StatusOK{_ = response.Body.Close();t.Fatal("authorized object retrieval did not succeed")}
  raw,e:=io.ReadAll(io.LimitReader(response.Body,artifact.SizeBytes+1));require.NoError(t,response.Body.Close());if e!=nil{t.Fatal("authorized object body read failed")};require.Equal(t,artifact.SizeBytes,int64(len(raw)))
  sum:=sha256.Sum256(raw);require.Equal(t,artifact.Sha256,hex.EncodeToString(sum[:]))
  require.NoError(t,os.WriteFile(filepath.Join(output,artifact.Filename),raw,0600));found[artifact.Filename]=true
  if artifact.ArtifactId==provider.Artifact.ID{require.Equal(t,provider.Artifact.SHA256,artifact.Sha256);require.Equal(t,provider.Artifact.Size,artifact.SizeBytes);require.Equal(t,provider.Artifact.Filename,artifact.Filename)}
 }
 require.True(t,found["model.pt"]);require.True(t,found["model_config.json"])
 // Removing only the content API grant cannot be replaced by a read grant.
 require.NoError(t,writer.Client().Api.UpdateOneID(apiIDs[2]).SetStatus(api.StatusOff).Exec(sys))
 code,body=get(downloadPath,token);require.Equal(t,http.StatusForbidden,code);require.NotContains(t,string(body),"download_url")
 code,_=get(detailPath,token);require.Equal(t,http.StatusOK,code)
 require.NoError(t,writer.Client().Api.UpdateOneID(apiIDs[2]).SetStatus(api.StatusOn).Exec(sys))
 // Current grant revocation wins over the still-valid JWT and cached Casbin policy.
 require.NoError(t,writer.Client().Permission.UpdateOneID(owner.permission).SetStatus(permission.StatusOff).Exec(sys))
 for _,path:=range []string{detailPath,artifactsPath,downloadPath}{code,body=get(path,token);require.Equal(t,http.StatusForbidden,code);require.NotContains(t,string(body),"download_url")}
 require.NoError(t,writer.Client().Permission.UpdateOneID(owner.permission).SetStatus(permission.StatusOn).Exec(sys))
 require.NoError(t,writer.Client().Role.UpdateOneID(owner.role).SetDataScope(role.DataScopeSelf).Exec(sys))
 code,body=get(downloadPath,token);require.Equal(t,http.StatusForbidden,code);require.NotContains(t,string(body),"download_url")
 t.Log("real JWT/Redis, Casbin, current PG grants, tenant-isolated ModelDev mTLS query and authorized HTTPS downloads passed; output ready for independent CPU forward")
}
