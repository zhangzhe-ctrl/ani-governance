//go:build quota_pg

package server

import (
 "context"
 "crypto/ecdsa"
 "crypto/elliptic"
 "crypto/rand"
 "crypto/tls"
 "crypto/x509"
 "crypto/x509/pkix"
 "encoding/pem"
 "math/big"
 "os"
 "path/filepath"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/stretchr/testify/require"
 "google.golang.org/grpc"
 "google.golang.org/grpc/codes"
 "google.golang.org/grpc/credentials"
 "google.golang.org/grpc/status"
 quotapb "go-wind-admin/api/gen/go/quota/service/v1"
 "go-wind-admin/app/admin/service/internal/data"
 "go-wind-admin/app/admin/service/internal/data/ent/quotacharge"
 "go-wind-admin/app/admin/service/tests/testutil"
 appViewer "go-wind-admin/pkg/entgo/viewer"
)

func issueQuotaCert(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, names []string, usage x509.ExtKeyUsage, dir, file string) {
 t.Helper()
 key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
 require.NoError(t, err)
 template := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), DNSNames: names, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
 der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
 require.NoError(t, err)
 require.NoError(t, os.WriteFile(filepath.Join(dir, file+".pem"), pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:der}), 0600))
 raw, err := x509.MarshalECPrivateKey(key)
 require.NoError(t, err)
 require.NoError(t, os.WriteFile(filepath.Join(dir, file+".key"), pem.EncodeToMemory(&pem.Block{Type:"EC PRIVATE KEY",Bytes:raw}), 0600))
}

func quotaTLSFixture(t *testing.T) (string, *x509.CertPool) {
 t.Helper()
 dir := t.TempDir()
 key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
 require.NoError(t, err)
 caTemplate := &x509.Certificate{SerialNumber:big.NewInt(1), Subject:pkix.Name{CommonName:"quota-test-ca"}, NotBefore:time.Now().Add(-time.Hour), NotAfter:time.Now().Add(time.Hour), IsCA:true, BasicConstraintsValid:true, KeyUsage:x509.KeyUsageCertSign|x509.KeyUsageDigitalSignature}
 der, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &key.PublicKey, key)
 require.NoError(t, err)
 ca, err := x509.ParseCertificate(der)
 require.NoError(t, err)
 caPEM := pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:der})
 require.NoError(t, os.WriteFile(filepath.Join(dir,"ca.pem"), caPEM, 0600))
 pool := x509.NewCertPool()
 require.True(t,pool.AppendCertsFromPEM(caPEM))
 issueQuotaCert(t,ca,key,[]string{"ani-governance"},x509.ExtKeyUsageServerAuth,dir,"server")
 issueQuotaCert(t,ca,key,[]string{"ani-inference"},x509.ExtKeyUsageClientAuth,dir,"owner")
 issueQuotaCert(t,ca,key,[]string{"other-service"},x509.ExtKeyUsageClientAuth,dir,"unknown")
 issueQuotaCert(t,ca,key,[]string{"ani-inference","other-owner"},x509.ExtKeyUsageClientAuth,dir,"mixed")
 issueQuotaCert(t,ca,key,[]string{"ani-inference","ani-inference-alias"},x509.ExtKeyUsageClientAuth,dir,"alias")
 return dir,pool
}

func TestQuotaInternalMTLSAndCumulativeRelease(t *testing.T) {
 c := testutil.NewQuotaPGClient(t)
 testutil.ResetQuotaFixture(t,c)
 ctx := appViewer.NewSystemViewerContext(context.Background())
 plan, err := c.Client().Plan.Create().SetName("quota-mtls").Save(ctx)
 require.NoError(t,err)
 tenant, err := c.Client().Tenant.Create().SetName("quota-mtls").SetCode("quota-mtls").SetPlanID(plan.ID).Save(ctx)
 require.NoError(t,err)
 require.NoError(t,c.Client().PlanQuota.Create().SetPlanID(plan.ID).SetQuotaCode(data.QuotaCodeGpuCount).SetQuotaValue(8).Exec(ctx))
 ledger := data.NewQuotaLedgerRepo(testutil.NewBootstrapContext(nil),c)
 occupied, err := ledger.Occupy(context.Background(),&data.QuotaOccupyInput{TenantID:tenant.ID,ResourceTenantID:tenant.ResourceTenantID,ResourceID:uuid.NewString(),ActorType:"user",ActorID:"1",OwnerService:"ani-inference",Action:"GPU_CREATE",IdempotencyKey:uuid.NewString(),RequestHash:"mtls-hash",CanonicalRequest:`{"schema_version":1}`,Items:[]data.QuotaOccupyItem{{QuotaCode:data.QuotaCodeGpuCount,Units:2}}})
 require.NoError(t,err)
 charge, err := c.Client().QuotaCharge.Query().Where(quotacharge.OperationIDEQ(occupied.OperationID)).Only(ctx)
 require.NoError(t,err)
 dir,pool := quotaTLSFixture(t)
 srv,err := NewQuotaInternalServer(QuotaInternalServerConfig{Enabled:true,Address:"127.0.0.1:0",CAFile:filepath.Join(dir,"ca.pem"),CertFile:filepath.Join(dir,"server.pem"),KeyFile:filepath.Join(dir,"server.key"),CertOwnerMap:map[string]string{"ani-inference":"ani-inference","ani-inference-alias":"ani-inference","other-owner":"other-owner"}},ledger)
 require.NoError(t,err)
 require.NoError(t,srv.Start(context.Background()))
 t.Cleanup(func(){_ = srv.Stop(context.Background())})
 dial := func(name string,roots *x509.CertPool) quotapb.QuotaReleaseServiceClient {
  t.Helper()
  cfg := &tls.Config{MinVersion:tls.VersionTLS13,RootCAs:roots,ServerName:"ani-governance"}
  if name != "" {pair,e:=tls.LoadX509KeyPair(filepath.Join(dir,name+".pem"),filepath.Join(dir,name+".key"));require.NoError(t,e);cfg.Certificates=[]tls.Certificate{pair}}
  conn,e:=grpc.NewClient(srv.listener.Addr().String(),grpc.WithTransportCredentials(credentials.NewTLS(cfg)))
  require.NoError(t,e)
  t.Cleanup(func(){_ = conn.Close()})
  return quotapb.NewQuotaReleaseServiceClient(conn)
 }
 call := func(client quotapb.QuotaReleaseServiceClient,event string,total int64) error {
  cctx,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel()
  _,e:=client.ReportQuotaRelease(cctx,&quotapb.ReportQuotaReleaseRequest{ReleaseEventId:event,OperationId:occupied.OperationID,Items:[]*quotapb.QuotaReleaseItem{{ChargeId:charge.ChargeID,QuotaCode:data.QuotaCodeGpuCount,ReleasedTotal:total}},Reason:quotapb.ReleaseReason_RESOURCE_RELEASED})
  return e
 }
 require.Error(t,call(dial("",pool),uuid.NewString(),1),"client cert required")
 require.Error(t,call(dial("owner",x509.NewCertPool()),uuid.NewString(),1),"trusted server CA required")
 require.Equal(t,codes.Unauthenticated,status.Code(call(dial("unknown",pool),uuid.NewString(),1)))
 require.Equal(t,codes.Unauthenticated,status.Code(call(dial("mixed",pool),uuid.NewString(),1)))
 balance := func() int64 {rows,e:=ledger.RecomputeInvariants(context.Background(),tenant.ID);require.NoError(t,e);for _,r:=range rows {require.True(t,r.Balanced);if r.QuotaCode==data.QuotaCodeGpuCount{return r.OccupiedUnits}};t.Fatal("missing gpu account");return -1}
 require.EqualValues(t,2,balance())
 owner:=dial("owner",pool)
 event:=uuid.NewString()
 require.NoError(t,call(owner,event,1))
 require.EqualValues(t,1,balance())
 require.NoError(t,call(owner,event,1))
 require.EqualValues(t,1,balance())
 require.NoError(t,call(dial("alias",pool),uuid.NewString(),2))
 require.EqualValues(t,0,balance())
 require.NoError(t,call(owner,uuid.NewString(),1))
 require.EqualValues(t,0,balance())
}
