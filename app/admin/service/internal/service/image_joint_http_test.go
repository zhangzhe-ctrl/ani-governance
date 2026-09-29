//go:build imageintegration

package service

import (
 "bytes"
 "context"
 "crypto/rand"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "io"
 "net/http"
 "net/http/httptest"
 "net/url"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "sync"
 "testing"
 "time"

 "entgo.io/ent/dialect"
 entsql "entgo.io/ent/dialect/sql"
 khttp "github.com/go-kratos/kratos/v2/transport/http"
 "github.com/redis/go-redis/v9"
 "github.com/stretchr/testify/require"
 admin "go-wind-admin/api/gen/go/admin/service/v1"
 auditv1 "go-wind-admin/api/gen/go/audit/service/v1"
 authv1 "go-wind-admin/api/gen/go/authentication/service/v1"
 "go-wind-admin/app/admin/service/cmd/server/assets"
 "go-wind-admin/app/admin/service/internal/data"
 "go-wind-admin/app/admin/service/internal/data/ent"
 "go-wind-admin/app/admin/service/internal/data/ent/accesskey"
 apiEntity "go-wind-admin/app/admin/service/internal/data/ent/api"
 "go-wind-admin/app/admin/service/internal/data/ent/permission"
 "go-wind-admin/app/admin/service/internal/data/ent/permissionapi"
 "go-wind-admin/app/admin/service/internal/data/ent/planmodule"
 "go-wind-admin/app/admin/service/internal/data/ent/role"
 "go-wind-admin/app/admin/service/internal/data/ent/rolepermission"
 "go-wind-admin/app/admin/service/internal/data/ent/tenant"
 "go-wind-admin/pkg/authorizer"
 appcrypto "go-wind-admin/pkg/crypto"
 appViewer "go-wind-admin/pkg/entgo/viewer"
 entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
 authzMiddleware "go-wind-admin/pkg/localdeps/kratos-authz/middleware"
 conf "go-wind-admin/pkg/localdeps/kratos-bootstrap/api/gen/go/conf/v1"
 "go-wind-admin/pkg/localdeps/kratos-bootstrap/bootstrap"
 bLogger "go-wind-admin/pkg/localdeps/kratos-bootstrap/logger"
 "go-wind-admin/pkg/middleware/auth"
 applogging "go-wind-admin/pkg/middleware/logging"
 dbbootstrap "go-wind-admin/sql/bootstrap"
)

// Run only through scripts/image-joint-integration: required inputs fail rather
// than skip. Auth/cache/policies/tenant resolution and HTTP/mTLS are real;
// Resource's protocol peer is deliberately not a Harbor/product acceptance.
func TestImageJointHTTP(t *testing.T){
 ctx:=context.Background();sys:=appViewer.NewSystemViewerContext(ctx)
 openDB:=func(env string)(*sql.DB,*entCrud.EntClient[*ent.Client],*ent.Client){
  t.Helper();file:=os.Getenv(env);require.NotEmpty(t,file,"isolated runner input required")
  raw,err:=os.ReadFile(file);require.NoError(t,err);db,err:=sql.Open("pgx",strings.TrimSpace(string(raw)));require.NoError(t,err);t.Cleanup(func(){_ = db.Close()})
  driver:=entsql.OpenDB(dialect.Postgres,db);client:=ent.NewClient(ent.Driver(driver));return db,entCrud.NewEntClient[*ent.Client](client,driver),client
 }
 runtimeDB,ec,_:=openDB("IMAGE_GOV_RUNTIME_DSN_FILE");fixtureDB,fixtureEC,client:=openDB("IMAGE_GOV_ADMIN_DSN_FILE")
 var privileged bool;require.NoError(t,runtimeDB.QueryRowContext(ctx,"SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolbypassrls FROM pg_roles WHERE rolname=current_user").Scan(&privileged));require.False(t,privileged)
 var owners int;require.NoError(t,runtimeDB.QueryRowContext(ctx,"SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace AND relowner=(SELECT oid FROM pg_roles WHERE rolname=current_user)").Scan(&owners));require.Zero(t,owners)
 randomKey:=make([]byte,32);_,err:=rand.Read(randomKey);require.NoError(t,err)
 cfg:=&conf.Bootstrap{Authz:&conf.Authorization{Type:"casbin"},Authn:&conf.Authentication{Jwt:&conf.Authentication_Jwt{Method:"HS256",Key:hex.EncodeToString(randomKey)}}}
 bctx:=bootstrap.NewContextWithParam(sys,nil,cfg,bLogger.NopLogger())
 raw,err:=os.ReadFile(os.Getenv("IMAGE_GOV_REDIS_FILE"));require.NoError(t,err);var redisCfg struct{Address,Password string};require.NoError(t,json.Unmarshal(raw,&redisCfg));require.NotEmpty(t,redisCfg.Password)
 rdb:=redis.NewClient(&redis.Options{Addr:redisCfg.Address,Password:redisCfg.Password});t.Cleanup(func(){_ = rdb.Close()});require.NoError(t,rdb.Ping(ctx).Err())
 cache:=data.NewUserTokenCache(bctx,rdb);authenticator:=data.NewAuthenticator(bctx,cache);checker:=data.NewTokenChecker(bctx,authenticator,authv1.ClientType_admin)
 apis:=data.NewApiRepo(bctx,ec);policy:=authorizer.NewAuthorizer(bctx,data.NewAuthorizerProvider(bctx,newRoleRepo(ec),apis))
 catalog,err:=dbbootstrap.Catalog(assets.OpenApiData);require.NoError(t,err);tx,err:=fixtureDB.BeginTx(ctx,nil);require.NoError(t,err);_,err=dbbootstrap.SyncAPIs(ctx,tx,catalog,true);require.NoError(t,err);require.NoError(t,tx.Rollback())
 require.NoError(t,data.NewApiRepo(bctx,fixtureEC).SyncOpenAPI(sys,assets.OpenApiData))
 seed,err:=os.ReadFile(filepath.Join("..","..","..","..","..","sql","data","20260930_image_permissions.sql"));require.NoError(t,err)
 for i:=0;i<2;i++{_,err=fixtureDB.ExecContext(ctx,string(seed));require.NoError(t,err)}
 count,err:=client.RolePermission.Query().Count(sys);require.NoError(t,err);require.Zero(t,count,"catalog import must not grant roles")
 plan,err:=client.Plan.Create().SetName("Image isolated fixture").Save(sys);require.NoError(t,err)
 module,err:=client.PlanModule.Create().SetPlanID(plan.ID).SetModule(planmodule.ModuleImage).Save(sys);require.NoError(t,err)
 uuids:=[]string{"11111111-1111-4111-8111-111111111111","22222222-2222-4222-8222-222222222222"}
 tenants:=make([]*ent.Tenant,2);for i,id:=range uuids{tenants[i],err=client.Tenant.Create().SetName(fmt.Sprintf("Image %d",i)).SetCode(fmt.Sprintf("image-%d",i)).SetResourceTenantID(id).SetStatus(tenant.StatusOn).SetPlanID(plan.ID).Save(sys);require.NoError(t,err)}
 createRole:=func(tid uint32,code string)*ent.Role{r,err:=client.Role.Create().SetTenantID(tid).SetCode(code).SetName(code).SetType(role.TypeTenant).SetStatus(role.StatusOn).Save(sys);require.NoError(t,err);return r}
 reader:=createRole(tenants[0].ID,"image-fixture-reader");publisher:=createRole(tenants[0].ID,"image-fixture-publisher");other:=createRole(tenants[1].ID,"image-fixture-reader")
 for _,op:=range auth.ImageOperations{
  a,err:=client.Api.Query().Where(apiEntity.PathEQ(op.Path),apiEntity.MethodEQ(op.Method)).Only(sys);require.NoError(t,err);require.Equal(t,apiEntity.BusinessModuleImage,*a.BusinessModule)
  perm,err:=client.Permission.Query().Where(permission.CodeEQ(op.Permission)).Only(sys);require.NoError(t,err)
  _,err=client.PermissionApi.Query().Where(permissionapi.APIIDEQ(a.ID),permissionapi.PermissionIDEQ(perm.ID)).Only(sys);require.NoError(t,err)
  grant:=[]*ent.Role{publisher};if op.ReadOnly{grant=append(grant,reader,other)}
  for _,r:=range grant{_,err=client.RolePermission.Create().SetTenantID(*r.TenantID).SetRoleID(r.ID).SetPermissionID(perm.ID).SetEffect(rolepermission.EffectAllow).SetStatus(rolepermission.StatusOn).Save(sys);require.NoError(t,err)}
 }
 require.NoError(t,policy.ResetPolicies(ctx))
 token:=func(r *ent.Role,uid uint32)string{v,_,err:=authenticator.CreateUserToken(ctx,authv1.ClientType_admin,&authv1.UserTokenPayload{UserId:uid,TenantId:r.TenantID,Roles:[]string{*r.Code}});require.NoError(t,err);return v}
 readerToken,publisherToken,otherToken:=token(reader,1),token(publisher,2),token(other,3)
 cipher,err:=appcrypto.NewAccessKeyCipher(hex.EncodeToString(randomKey));require.NoError(t,err);keys:=data.NewAccessKeyRepo(bctx,ec,cipher)
 makeAK:=func(r *ent.Role,suffix string)(*ent.AccessKey,string){secret:="sk-"+hex.EncodeToString(randomKey)+suffix;encrypted,err:=cipher.Encrypt(secret);require.NoError(t,err);k,err:=client.AccessKey.Create().SetTenantID(*r.TenantID).SetRoleID(r.ID).SetAccessKey("ak-image-"+suffix).SetSecretCiphertext(encrypted).SetStatus(accesskey.StatusOn).Save(sys);require.NoError(t,err);return k,secret}
 readerAK,readerSK:=makeAK(reader,"reader");publisherAK,publisherSK:=makeAK(publisher,"publisher")
 peerClient,peer:=newImageJointPeer(t,"ani-network-service")
 var auditMu sync.Mutex;var auditRows []*auditv1.ApiAuditLog
 serverFor:=func(downstream *data.ImageClient)*httptest.Server{
  server:=khttp.NewServer(khttp.Filter(auth.ImageHTTPFilter),khttp.Middleware(applogging.Server(applogging.WithWriteApiLogFunc(func(_ context.Context,v *auditv1.ApiAuditLog)error{auditMu.Lock();auditRows=append(auditRows,v);auditMu.Unlock();return nil})),auth.CredentialHeaders(),auth.Server(auth.WithAccessTokenChecker(checker),auth.WithSigningKeyStore(keys),auth.WithTenantAccessChecker(data.NewTenantAccessCheckerImpl(bctx,ec)),auth.WithInjectMetadata(false)),authzMiddleware.Server(policy.Engine())))
  admin.RegisterImageServiceHTTPServer(server,NewImageService(downstream,data.NewTenantRepo(bctx,ec)));host:=httptest.NewServer(server);t.Cleanup(host.Close);return host
 }
 host:=serverFor(peerClient)
 request:=func(host *httptest.Server,method,path,bearer,body string,key *ent.AccessKey,secret string,mutate func(*http.Request),want int)map[string]any{
  t.Helper();req,err:=http.NewRequest(method,host.URL+path,strings.NewReader(body));require.NoError(t,err);req.Header.Set("Content-Type","application/json");req.Header.Set("Referer","https://example.test/?secret=referer-sentinel")
  if bearer!=""{req.Header.Set("Authorization","Bearer "+bearer)};if key!=nil{u,err:=url.Parse(path);require.NoError(t,err);ts:=strconv.FormatInt(time.Now().Unix(),10);req.Header.Set("X-Access-Key",key.AccessKey);req.Header.Set("X-Timestamp",ts);req.Header.Set("X-Signature",auth.ImageSignature(secret,method,u.Path,u.RawQuery,key.AccessKey,ts,[]byte(body)))}
  if mutate!=nil{mutate(req)};resp,err:=host.Client().Do(req);require.NoError(t,err);defer resp.Body.Close();payload,err:=io.ReadAll(resp.Body);require.NoError(t,err)
  require.Equal(t,want,resp.StatusCode,method+" "+path);if resp.StatusCode!=want{t.Fatal("unexpected HTTP status; response intentionally excluded from logs")}
  if strings.Contains(path,"publisher-credential"){require.Equal(t,"no-store",resp.Header.Get("Cache-Control"));require.Equal(t,"no-cache",resp.Header.Get("Pragma"))}
  require.NotContains(t,string(payload),"tenantId");require.NotContains(t,string(payload),"tenant_id")
  if method=="GET"||want!=200{require.NotContains(t,string(payload),"joint-delivery-secret-sentinel")}
  t.Logf("HTTP %s %s -> %d",method,path,want);out:=map[string]any{};require.NoError(t,json.Unmarshal(payload,&out));return out
 }
 get:=func(path,bearer string,key *ent.AccessKey,secret string,want int)map[string]any{return request(host,"GET",path,bearer,"",key,secret,nil,want)}
 callCount:=func()int{peer.mu.Lock();defer peer.mu.Unlock();return peer.calls}
 get("/api/v1/images/publisher-credential","",nil,"",401)
 out:=get("/api/v1/images/space",readerToken,nil,"",200);require.Equal(t,"1",out["space"].(map[string]any)["version"])
 get("/api/v1/images/publisher-credential",readerToken,nil,"",200)
 get("/api/v1/images/registrations?scope=tenant",readerToken,nil,"",200)
 get("/api/v1/images/registrations?scope=platform",readerToken,nil,"",200)
 get("/api/v1/images/registrations/img_"+strings.Repeat("a",32)+"?scope=tenant",readerToken,nil,"",200)
 get("/api/v1/images/space",otherToken,nil,"",200)
 get("/api/v1/images/space","",readerAK,readerSK,200);peer.mu.Lock();require.Equal(t,"governance:access-key:"+strconv.FormatUint(uint64(readerAK.ID),10),peer.actor);peer.mu.Unlock()
 issueBody:=`{"expectedVersion":"1","idempotencyKey":"joint-issue-1"}`
 before:=callCount();request(host,"POST","/api/v1/images/publisher-credential:issue",readerToken,issueBody,nil,"",nil,403);request(host,"POST","/api/v1/images/publisher-credential:issue","",issueBody,readerAK,readerSK,nil,403);require.Equal(t,before,callCount())
 for _,op:=range []string{"issue","reset"}{v:=request(host,"POST","/api/v1/images/publisher-credential:"+op,publisherToken,issueBody,nil,"",nil,200);require.Equal(t,"joint-delivery-secret-sentinel",v["secret"])}
 request(host,"POST","/api/v1/images/publisher-credential:issue","",issueBody,publisherAK,publisherSK,nil,200)
 for _,tc:=range []struct{method,path,body string}{
  {"POST","/api/v1/images/space:enable",`{"slug":"example","idempotencyKey":"joint-enable"}`},
  {"POST","/api/v1/images/publisher-credential:disable",issueBody},
  {"POST","/api/v1/images/registrations",`{"imageReference":"registry.example.test/t-example/app:v1","displayName":"Example","purposes":["container"],"idempotencyKey":"joint-register"}`},
  {"PATCH","/api/v1/images/registrations/img_"+strings.Repeat("a",32),`{"displayName":"Edited","purposes":["container"],"expectedVersion":"1","idempotencyKey":"joint-update"}`},
  {"POST","/api/v1/images/registrations/img_"+strings.Repeat("a",32)+":unregister",issueBody},
 }{request(host,tc.method,tc.path,publisherToken,tc.body,nil,"",nil,200)}
 before=callCount()
 for _,header:=range []string{"X-Ani-Tenant-Id","X-Ani-Actor","X-Tenant-Id"}{request(host,"POST","/api/v1/images/publisher-credential:issue",publisherToken,issueBody,nil,"",func(r *http.Request){r.Header.Add(header,"forged");r.Header.Add(header,"duplicate")},400)}
 request(host,"POST","/api/v1/images/publisher-credential:issue",publisherToken,`{"tenantId":"forged","idempotencyKey":"x"}`,nil,"",nil,400)
 request(host,"POST","/api/v1/images/publisher-credential:issue","",issueBody,publisherAK,publisherSK,func(r *http.Request){changed:=bytes.ReplaceAll([]byte(issueBody),[]byte("joint-issue-1"),[]byte("joint-issue-2"));r.Body=io.NopCloser(bytes.NewReader(changed));r.ContentLength=int64(len(changed))},401)
 request(host,"POST","/api/v1/images/publisher-credential:issue",publisherToken,issueBody,publisherAK,publisherSK,nil,400)
 request(host,"GET","/api/v1/images/registrations?scope=tenant&search=x","","",readerAK,readerSK,nil,200)
 require.Equal(t,before+1,callCount())
 peer.mu.Lock();peer.badTenant=true;peer.mu.Unlock();get("/api/v1/images/space",readerToken,nil,"",503);peer.mu.Lock();peer.badTenant=false;peer.mu.Unlock()
 wrong,_:=newImageJointPeer(t,"wrong-resource");request(serverFor(wrong),"GET","/api/v1/images/space",readerToken,"",nil,"",nil,503)
 _,err=client.RolePermission.Update().Where(rolepermission.RoleIDEQ(publisher.ID)).SetStatus(rolepermission.StatusOff).Save(sys);require.NoError(t,err);require.NoError(t,policy.ResetPolicies(ctx));before=callCount();request(host,"POST","/api/v1/images/publisher-credential:issue","",issueBody,publisherAK,publisherSK,nil,403);request(host,"POST","/api/v1/images/publisher-credential:issue",publisherToken,issueBody,nil,"",nil,403);require.Equal(t,before,callCount())
 _,err=client.AccessKey.UpdateOneID(readerAK.ID).SetStatus(accesskey.StatusOff).Save(sys);require.NoError(t,err);get("/api/v1/images/space","",readerAK,readerSK,401)
 require.NoError(t,client.PlanModule.DeleteOneID(module.ID).Exec(sys));get("/api/v1/images/space",readerToken,nil,"",403)
 auditMu.Lock();defer auditMu.Unlock();require.NotEmpty(t,auditRows);for _,v:=range auditRows{require.Equal(t,"[redacted]",v.GetRequestBody());require.Empty(t,v.GetReferer());require.NotContains(t,v.String(),"joint-delivery-secret-sentinel");require.NotContains(t,v.String(),publisherToken);require.NotContains(t,v.String(),publisherSK);require.NotContains(t,v.String(),"referer-sentinel")}
}
