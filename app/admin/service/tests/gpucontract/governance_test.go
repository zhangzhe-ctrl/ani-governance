package gpucontract

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	_ "github.com/jackc/pgx/v5/stdlib"
	acc "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/server"
	"go-wind-admin/app/admin/service/internal/service"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
	"go-wind-admin/pkg/localdeps/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/pkg/localdeps/kratos-bootstrap/logger"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type testGovConfig struct {
	Address, ReleaseAddress, OwnerAddress, CA, Cert, Key string
	Accelerator                                          data.AcceleratorClientConfig
	StartPaused                                          bool
}

func jointLedger(t *testing.T) (*data.QuotaLedgerRepo, *entCrud.EntClient[*ent.Client]) {
	t.Helper()
	dsn, e := secretFile("gov-dsn")
	if e != nil {
		t.Fatal(e)
	}
	db, e := sql.Open("pgx", dsn)
	if e != nil {
		t.Fatal("cannot open joint governance database")
	}
	t.Cleanup(func() { _ = db.Close() })
	drv := entsql.OpenDB("postgres", db)
	c := ent.NewClient(ent.Driver(drv))
	client := entCrud.NewEntClient(c, drv)
	return data.NewQuotaLedgerRepoForTest(client, bLogger.NewHelper(bLogger.NopLogger())), client
}

// TestJointGovernanceProcess hosts real production acceptance/dispatch/sync and
// release code behind a private test control listener. It is not a product API.
func TestJointGovernanceProcess(t *testing.T) {
	mode := os.Getenv("GOV_ACC_PROCESS")
	if !strings.HasPrefix(mode, "governance") {
		t.Skip("explicit subprocess helper")
	}
	cfg, e := readConfig[testGovConfig]("governance-config.json")
	if e != nil {
		t.Fatal(e)
	}
	if mode == "governance2" || mode == "governance-nosync2" {
		cfg.Address = "127.0.0.1:25564"
		cfg.ReleaseAddress = "127.0.0.1:25565"
	}
	token, e := secretFile("control-token")
	if e != nil || token == "" {
		t.Fatal("private control token required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	ledger, entClient := jointLedger(t)
	bctx := bootstrap.NewContextWithParam(ctx, nil, nil, bLogger.NopLogger())
	tenantRepo := data.NewTenantRepo(bctx, entClient)
	accClient, closeAcc, e := data.NewAcceleratorClient(cfg.Accelerator)
	if e != nil {
		t.Fatal(e)
	}
	defer closeAcc()
	ownerTLS, e := testTLS(cfg.CA, cfg.Cert, cfg.Key, "ani-inference")
	if e != nil {
		t.Fatal(e)
	}
	adapter := &ownerAdapter{client: &http.Client{Transport: &http.Transport{TLSClientConfig: ownerTLS}, Timeout: 3 * time.Second}, address: cfg.OwnerAddress}
	registry := service.NewQuotaAdapterRegistry()
	if e = registry.Register(adapter); e != nil {
		t.Fatal(e)
	}
	worker := service.NewQuotaDispatchWorker(bctx, ledger, registry)
	worker.SetPaused(cfg.StartPaused)
	acceptance, e := service.NewGpuAcceptance(ledger, registry, tenantRepo, accClient.Catalog, adapter, worker)
	if e != nil {
		t.Fatal(e)
	}
	disabled, e := service.NewGpuAcceptance(ledger, service.NewQuotaAdapterRegistry(), tenantRepo, nil, adapter, worker)
	if e != nil {
		t.Fatal(e)
	}
	syncWorker := service.NewGpuUsageSyncWorker(ledger, accClient, func(e error) { t.Log("projection retry", e) })
	release, e := server.NewQuotaInternalServer(server.QuotaInternalServerConfig{Enabled: true, Address: cfg.ReleaseAddress, CAFile: cfg.CA, CertFile: cfg.Cert, KeyFile: cfg.Key, CertOwnerMap: map[string]string{"ani-inference": "ani-inference"}}, ledger)
	if e != nil {
		t.Fatal(e)
	}
	if e = release.Start(ctx); e != nil {
		t.Fatal(e)
	}
	defer release.Stop(context.Background())
	if e = worker.Start(ctx); e != nil {
		t.Fatal(e)
	}
	defer worker.Stop(context.Background())
	if mode != "governance-nosync" && mode != "governance-nosync2" {
		if e = syncWorker.Start(ctx); e != nil {
			t.Fatal(e)
		}
		defer syncWorker.Stop(context.Background())
	}
	mux := http.NewServeMux()
	respond := func(w http.ResponseWriter, value any, e error) {
		w.Header().Set("Content-Type", "application/json")
		if e != nil {
			status := int(kerrors.Code(e))
			if status < 400 || status > 599 {
				status = 500
			}
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": kerrors.Reason(e)})
			return
		}
		_ = json.NewEncoder(w).Encode(value)
	}
	principal := func(r *http.Request) context.Context {
		return auth.NewPrincipalContext(r.Context(), &auth.Principal{Type: auth.SubjectUser, ID: 1, TenantID: 1})
	}
	mux.HandleFunc("/create", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Key, Name string
			Gpu       *acc.GpuRequest
		}
		if e := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); e != nil {
			http.Error(w, "bad request", 400)
			return
		}
		result, e := acceptance.AcceptGpuCreate(principal(r), req.Key, req.Gpu, wrapperspb.String(req.Name))
		respond(w, result, e)
	})
	mux.HandleFunc("/create-disabled", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Key, Name string
			Gpu       *acc.GpuRequest
		}
		if e := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); e != nil {
			http.Error(w, "bad request", 400)
			return
		}
		result, e := disabled.AcceptGpuCreate(principal(r), req.Key, req.Gpu, wrapperspb.String(req.Name))
		respond(w, result, e)
	})
	mux.HandleFunc("/create-denied", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Key, Name string
			Gpu       *acc.GpuRequest
		}
		if e := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); e != nil {
			http.Error(w, "bad request", 400)
			return
		}
		denied := auth.NewPrincipalContext(r.Context(), &auth.Principal{Type: auth.SubjectUser, ID: 2, TenantID: 1})
		result, e := acceptance.AcceptGpuCreate(denied, req.Key, req.Gpu, wrapperspb.String(req.Name))
		respond(w, result, e)
	})
	mux.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Key, Resource string }
		if e := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); e != nil {
			http.Error(w, "bad request", 400)
			return
		}
		result, e := acceptance.AcceptGpuDelete(principal(r), req.Key, req.Resource)
		respond(w, result, e)
	})
	mux.HandleFunc("/pause", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Paused bool }
		if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
			http.Error(w, "bad request", 400)
			return
		}
		worker.SetPaused(req.Paused)
		respond(w, map[string]bool{"paused": worker.Paused()}, nil)
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { respond(w, map[string]bool{"test_helper": true}, nil) })
	lis, e := net.Listen("tcp", cfg.Address)
	if e != nil {
		t.Fatal(e)
	}
	srv := &http.Server{ReadHeaderTimeout: 3 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Control") != token {
			http.Error(w, "forbidden", 403)
			return
		}
		mux.ServeHTTP(w, r)
	})}
	go srv.Serve(lis)
	if e = os.WriteFile(filepath.Join(os.Getenv("GOV_ACC_JOINT_DIR"), mode+"-ready"), []byte(lis.Addr().String()), 0600); e != nil {
		t.Fatal(e)
	}
	<-ctx.Done()
	shutdown, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	_ = srv.Shutdown(shutdown)
}
