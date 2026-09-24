package gpucontract

// This independent persistent test owner exists only in a Go test executable.
// It represents command acceptance/closing/outbox software, never GPU hardware.
import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	quota "go-wind-admin/api/gen/go/quota/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"
	"go-wind-admin/pkg/middleware/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

//go:embed testdata/owner-queries.sql
var ownerQueries embed.FS

func ownerSQL(name string) string {
	b, e := ownerQueries.ReadFile("testdata/owner-queries.sql")
	if e != nil {
		panic(e)
	}
	for _, section := range strings.Split(string(b), "-- name: ")[1:] {
		title, body, _ := strings.Cut(section, "\n")
		if strings.TrimSpace(title) == name {
			return strings.TrimSpace(body)
		}
	}
	panic("missing test query " + name)
}

func secretFile(name string) (string, error) {
	b, e := os.ReadFile(filepath.Join(os.Getenv("GOV_ACC_JOINT_DIR"), name))
	return strings.TrimSpace(string(b)), e
}
func testTLS(ca, cert, key, serverName string) (*tls.Config, error) {
	pem, e := os.ReadFile(ca)
	if e != nil {
		return nil, e
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("empty test CA")
	}
	pair, e := tls.LoadX509KeyPair(cert, key)
	if e != nil {
		return nil, e
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ClientCAs: roots, Certificates: []tls.Certificate{pair}, ServerName: serverName}, nil
}

type testOwnerConfig struct{ Address, CA, Cert, Key, GovernanceAddress string }

func readConfig[T any](name string) (T, error) {
	var c T
	b, e := os.ReadFile(filepath.Join(os.Getenv("GOV_ACC_JOINT_DIR"), name))
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	return c, e
}

type persistentOwner struct {
	pool    *pgxpool.Pool
	release quota.QuotaReleaseServiceClient
}

func (o *persistentOwner) accept(ctx context.Context, cmd *service.QuotaDispatchCommand) ([]byte, error) {
	create, del, e := service.BuildGpuOwnerAttachments("ani-inference", cmd)
	if e != nil {
		return nil, e
	}
	if (cmd.Action != "TEST_GPU_CREATE" && cmd.Action != "TEST_GPU_DELETE") || (cmd.Action == "TEST_GPU_CREATE" && create == nil) || (cmd.Action == "TEST_GPU_DELETE" && del == nil) {
		return nil, fmt.Errorf("invalid test owner action")
	}
	createID := cmd.OperationID
	kind := "CREATE"
	if del != nil {
		createID = cmd.CreateOperationID
		kind = "DELETE"
	}
	tx, e := o.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	_, e = tx.Exec(ctx, ownerSQL("EnsureResource"), cmd.ResourceTenantID, cmd.ResourceID, createID, string(cmd.CanonicalRequest))
	if e != nil {
		return nil, e
	}
	var originalID, canonical string
	var closed, executed bool
	e = tx.QueryRow(ctx, ownerSQL("LockResource"), cmd.ResourceTenantID, cmd.ResourceID).Scan(&originalID, &canonical, &closed, &executed)
	if e != nil {
		return nil, e
	}
	if originalID != createID || canonical != string(cmd.CanonicalRequest) {
		return nil, fmt.Errorf("immutable owner identity conflict")
	}
	var hash, ack string
	e = tx.QueryRow(ctx, ownerSQL("GetCommand"), cmd.ResourceTenantID, cmd.OperationID).Scan(&hash, &ack)
	if e == nil {
		if hash != cmd.RequestHash {
			return nil, fmt.Errorf("owner idempotency conflict")
		}
		return []byte(ack), tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return nil, e
	}
	ackRaw, e := json.Marshal(map[string]any{"operation_id": cmd.OperationID, "resource_id": cmd.ResourceID, "accepted": true})
	if e != nil {
		return nil, e
	}
	_, e = tx.Exec(ctx, ownerSQL("SaveCommand"), cmd.ResourceTenantID, cmd.OperationID, cmd.ResourceID, createID, cmd.RequestHash, kind, string(ackRaw))
	if e != nil {
		return nil, e
	}
	if kind == "CREATE" {
		// A closed tombstone wins even when DELETE preceded CREATE. The fixture
		// only records software execution; no external workload is ever created.
		if !closed {
			_, e = tx.Exec(ctx, ownerSQL("ExecuteCreate"), cmd.ResourceTenantID, cmd.ResourceID, createID)
		}
	} else {
		_, e = tx.Exec(ctx, ownerSQL("CloseResource"), cmd.ResourceTenantID, cmd.ResourceID, createID)
		if e != nil {
			return nil, e
		}
		event := uuid.NewSHA1(uuid.NameSpaceOID, []byte("test-gpu-release:"+cmd.ResourceTenantID+":"+createID)).String()
		notice := &quota.ReportQuotaReleaseRequest{ReleaseEventId: event, OperationId: createID, Reason: quota.ReleaseReason_RESOURCE_RELEASED}
		for _, q := range del.OriginalGpuCharges {
			notice.Items = append(notice.Items, &quota.QuotaReleaseItem{ChargeId: q.ChargeId, QuotaCode: q.QuotaCode, ReleasedTotal: q.OriginalUnits})
		}
		payload, marshalErr := json.Marshal(notice)
		if marshalErr != nil {
			return nil, marshalErr
		}
		_, e = tx.Exec(ctx, ownerSQL("SaveNotification"), cmd.ResourceTenantID, event, cmd.ResourceID, createID, string(payload))
	}
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return ackRaw, nil
}

func (o *persistentOwner) notify(ctx context.Context) error {
	rows, e := o.pool.Query(ctx, ownerSQL("ScanNotifications"))
	if e != nil {
		return e
	}
	type pending struct{ tenant, event, payload string }
	var queue []pending
	for rows.Next() {
		var p pending
		if e = rows.Scan(&p.tenant, &p.event, &p.payload); e != nil {
			rows.Close()
			return e
		}
		queue = append(queue, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, p := range queue {
		var req quota.ReportQuotaReleaseRequest
		if e = json.Unmarshal([]byte(p.payload), &req); e != nil {
			return e
		}
		callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, e = o.release.ReportQuotaRelease(callCtx, &req)
		cancel()
		// OS-process fault barrier after the real Gov receipt commits, before
		// the independent owner's durable outbox acknowledgement.
		marker := filepath.Join(os.Getenv("GOV_ACC_JOINT_DIR"), "owner-drop-release-response")
		if e == nil {
			if _, exists := os.Stat(marker); exists == nil {
				_ = os.Remove(marker)
				_ = os.WriteFile(marker+".committed", []byte(p.event), 0600)
				<-ctx.Done()
				return ctx.Err()
			}
		}
		query := "AckNotification"
		if e != nil {
			query = "RetryNotification"
		}
		if _, e = o.pool.Exec(ctx, ownerSQL(query), p.tenant, p.event); e != nil {
			return e
		}
	}
	return nil
}

func TestJointOwnerProcess(t *testing.T) {
	if os.Getenv("GOV_ACC_PROCESS") != "owner" {
		t.Skip("explicit subprocess helper")
	}
	cfg, e := readConfig[testOwnerConfig]("owner-config.json")
	if e != nil {
		t.Fatal(e)
	}
	dsn, e := secretFile("owner-dsn")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal("owner database connection failed")
	}
	defer pool.Close()
	if e = pool.Ping(ctx); e != nil {
		t.Fatal("owner database unavailable")
	}
	tlsConfig, e := testTLS(cfg.CA, cfg.Cert, cfg.Key, "ani-governance")
	if e != nil {
		t.Fatal(e)
	}
	conn, e := grpc.NewClient(cfg.GovernanceAddress, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	owner := &persistentOwner{pool: pool, release: quota.NewQuotaReleaseServiceClient(conn)}
	serverTLS := tlsConfig.Clone()
	serverTLS.ServerName = ""
	serverTLS.ClientAuth = tls.RequireAndVerifyClientCert
	lis, e := net.Listen("tcp", cfg.Address)
	if e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/command", func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
			http.Error(w, "client required", 401)
			return
		}
		leaf := r.TLS.VerifiedChains[0][0]
		if len(leaf.URIs) != 1 || leaf.URIs[0].String() != data.GovernanceAcceleratorURI {
			http.Error(w, "wrong caller", 403)
			return
		}
		var cmd service.QuotaDispatchCommand
		decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if e := decoder.Decode(&cmd); e != nil {
			http.Error(w, "invalid command", 400)
			return
		}
		ack, e := owner.accept(r.Context(), &cmd)
		if e != nil {
			http.Error(w, "owner rejected command", 409)
			return
		}
		hold := filepath.Join(os.Getenv("GOV_ACC_JOINT_DIR"), "owner-hold-ack")
		if _, err := os.Stat(hold); err == nil {
			_ = os.WriteFile(hold+".entered", []byte(cmd.OperationID), 0600)
			for {
				if _, err := os.Stat(hold); os.IsNotExist(err) {
					break
				}
				select {
				case <-r.Context().Done():
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
		}
		drop := filepath.Join(os.Getenv("GOV_ACC_JOINT_DIR"), "owner-drop-next-ack")
		if _, e = os.Stat(drop); e == nil {
			_ = os.Remove(drop)
			if h, ok := w.(http.Hijacker); ok {
				c, _, err := h.Hijack()
				if err == nil {
					_ = c.Close()
					return
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		invalid := filepath.Join(os.Getenv("GOV_ACC_JOINT_DIR"), "owner-invalid-ack")
		if mode, err := os.ReadFile(invalid); err == nil {
			_ = os.Remove(invalid)
			value := map[string]any{"operation_id": cmd.OperationID, "resource_id": cmd.ResourceID, "accepted": true}
			switch string(mode) {
			case "operation":
				value["operation_id"] = uuid.NewString()
			case "resource":
				value["resource_id"] = uuid.NewString()
			case "accepted":
				value["accepted"] = false
			case "json":
				_, _ = w.Write([]byte("not-json"))
				return
			}
			ack, _ = json.Marshal(value)
		}
		_, _ = w.Write(ack)
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 3 * time.Second}
	go srv.Serve(tls.NewListener(lis, serverTLS))
	if e = os.WriteFile(filepath.Join(os.Getenv("GOV_ACC_JOINT_DIR"), "owner-ready"), []byte(lis.Addr().String()), 0600); e != nil {
		t.Fatal(e)
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			shutdown, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			_ = srv.Shutdown(shutdown)
			return
		case <-ticker.C:
			if e = owner.notify(ctx); e != nil && ctx.Err() == nil {
				t.Log("owner notification storage retry")
			}
		}
	}
}

type ownerAdapter struct {
	client  *http.Client
	address string
}

func (*ownerAdapter) OwnerService() string { return "ani-inference" }
func (*ownerAdapter) CreateAction() string { return "TEST_GPU_CREATE" }
func (*ownerAdapter) DeleteAction() string { return "TEST_GPU_DELETE" }
func (*ownerAdapter) GpuQuotaCodes() []string {
	return []string{service.GpuPhysicalQuotaCode, service.GpuSharedQuotaCode}
}
func (a *ownerAdapter) Actions() []string { return []string{a.CreateAction(), a.DeleteAction()} }
func (*ownerAdapter) AuthorizeGpu(_ context.Context, p *auth.Principal, action, resource string) error {
	if p == nil || p.Type != auth.SubjectUser || p.ID != 1 || p.TenantID != 1 {
		return data.QuotaErrNotFound("resource not found")
	}
	return nil
}
func (*ownerAdapter) ValidateGpuBusiness(_ context.Context, p proto.Message) ([]data.QuotaOccupyItem, error) {
	v, ok := p.(*wrapperspb.StringValue)
	if !ok || v.Value == "" {
		return nil, data.QuotaErrInvalid("test business name required")
	}
	if strings.HasPrefix(v.Value, "mixed-") {
		return []data.QuotaOccupyItem{{QuotaCode: "storage.bytes", Units: 10}}, nil
	}
	return nil, nil
}
func (a *ownerAdapter) Dispatch(ctx context.Context, cmd *service.QuotaDispatchCommand) ([]byte, error) {
	if _, _, e := service.BuildGpuOwnerAttachments(a.OwnerService(), cmd); e != nil {
		return nil, e
	}
	b, e := json.Marshal(cmd)
	if e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, a.address+"/command", bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	res, e := a.client.Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("test owner status %d", res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, 1<<20))
}
