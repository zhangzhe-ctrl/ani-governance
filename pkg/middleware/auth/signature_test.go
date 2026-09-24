package auth

import (
	"context"
	"errors"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"go-wind-admin/pkg/localdeps/go-crud/viewer"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const vectorPath = "/api/v1/networks/vpcs/vpc_0123456789abcdef0123456789abcdef"
const vectorAK = "ak-doc-example"
const vectorSK = "sk-doc-example-not-a-real-secret"

type signingStoreStub struct {
	key      *SigningKey
	err      error
	touchErr error
	touched  int
}

func (s *signingStoreStub) LookupSigningKey(context.Context, string) (*SigningKey, error) {
	return s.key, s.err
}
func (s *signingStoreStub) MarkSigningKeyUsed(context.Context, uint32) error {
	s.touched++
	return s.touchErr
}
func signedRequest(timestamp int64) *http.Request {
	r := httptest.NewRequest("GET", vectorPath, nil)
	ts := strconv.FormatInt(timestamp, 10)
	r.Header.Set("X-Access-Key", vectorAK)
	r.Header.Set("X-Timestamp", ts)
	r.Header.Set("X-Signature", VPCSignature(vectorSK, vectorPath, vectorAK, ts))
	return r
}
func TestVPCSignatureFixedVector(t *testing.T) {
	if n := len(CanonicalVPCRequest(vectorPath, vectorAK, "1700000000")); n != 170 {
		t.Fatalf("canonical bytes %d", n)
	}
	if got := VPCSignature(vectorSK, vectorPath, vectorAK, "1700000000"); got != "7df61f06b799ae477b51975097825e2e154ff62dffc46842964a51ed1afaebeb" {
		t.Fatal("signature vector mismatch")
	}
}
func TestSignatureBoundariesAndReadReplay(t *testing.T) {
	now := time.Unix(1700000000, 0)
	store := &signingStoreStub{key: &SigningKey{ID: 9, TenantID: 5, RoleAllowed: true, Role: "tenant:vpc-reader", Secret: vectorSK}}
	for _, delta := range []int64{-301, -300, 0, 300, 301} {
		r := signedRequest(now.Unix() + delta)
		p, err := verifySignature(context.Background(), r, VPCReadOperation, store, now)
		if delta < -300 || delta > 300 {
			if kerrors.Code(err) != 401 {
				t.Fatalf("delta %d accepted", delta)
			}
			continue
		}
		if err != nil || p.Type != SubjectAPIKey || p.ID != 9 || p.TenantID != 5 {
			t.Fatalf("delta %d: %v", delta, err)
		}
	}
	for range 2 {
		if _, err := verifySignature(context.Background(), signedRequest(now.Unix()), VPCReadOperation, store, now); err != nil {
			t.Fatal(err)
		}
	}
}
func TestSignatureRejections(t *testing.T) {
	now := time.Unix(1700000000, 0)
	tests := []struct {
		name   string
		code   int
		mutate func(*http.Request)
	}{
		{"wrong signature", 401, func(r *http.Request) { r.Header.Set("X-Signature", strings.Repeat("0", 64)) }},
		{"path tamper", 401, func(r *http.Request) { r.URL.Path = strings.Replace(r.URL.Path, "012345", "112345", 1) }},
		{"timestamp tamper", 401, func(r *http.Request) { r.Header.Set("X-Timestamp", "1700000001") }},
		{"leading zero", 401, func(r *http.Request) { r.Header.Set("X-Timestamp", "01700000000") }},
		{"overflow", 401, func(r *http.Request) { r.Header.Set("X-Timestamp", "9223372036854775808") }},
		{"negative", 401, func(r *http.Request) { r.Header.Set("X-Timestamp", "-1") }},
		{"uppercase signature", 401, func(r *http.Request) { r.Header.Set("X-Signature", strings.ToUpper(r.Header.Get("X-Signature"))) }},
		{"query", 400, func(r *http.Request) { r.URL.RawQuery = "x=1" }},
		{"empty query marker", 400, func(r *http.Request) { r.URL.ForceQuery = true }},
		{"encoded path", 400, func(r *http.Request) { r.URL.RawPath = "/api/v1/networks/vpcs/%76pc_0123456789abcdef0123456789abcdef" }},
		{"body", 400, func(r *http.Request) { r.Body = httptest.NewRequest("GET", "/", strings.NewReader("x")).Body }},
	}
	for _, header := range []string{"X-Access-Key", "X-Signature", "X-Timestamp"} {
		tests = append(tests, struct {
			name   string
			code   int
			mutate func(*http.Request)
		}{"missing " + header, 401, func(r *http.Request) { r.Header.Del(header) }}, struct {
			name   string
			code   int
			mutate func(*http.Request)
		}{"duplicate " + header, 401, func(r *http.Request) { r.Header.Add(header, r.Header.Get(header)) }}, struct {
			name   string
			code   int
			mutate func(*http.Request)
		}{"combined " + header, 401, func(r *http.Request) { r.Header.Set(header, r.Header.Get(header)+","+r.Header.Get(header)) }})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := signedRequest(now.Unix())
			tc.mutate(r)
			store := &signingStoreStub{key: &SigningKey{ID: 9, TenantID: 5, RoleAllowed: true, Role: "reader", Secret: vectorSK}}
			_, err := verifySignature(context.Background(), r, VPCReadOperation, store, now)
			if kerrors.Code(err) != tc.code || store.touched != 0 {
				t.Fatalf("status %d want %d touched %d", kerrors.Code(err), tc.code, store.touched)
			}
		})
	}
}
func TestSigningStoreFailureAndUserOnly(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		store *signingStoreStub
		code  int
	}{
		{&signingStoreStub{err: ErrSigningKeyRejected}, 401},
		{&signingStoreStub{err: errors.New("database offline")}, 503},
		{&signingStoreStub{key: &SigningKey{ID: 9, TenantID: 5, RoleAllowed: true, Role: "reader"}}, 503},
		{&signingStoreStub{key: &SigningKey{ID: 9, TenantID: 5, RoleAllowed: true, Role: "reader", Secret: vectorSK}, touchErr: errors.New("database offline")}, 503},
	} {
		if _, err := verifySignature(context.Background(), signedRequest(now.Unix()), VPCReadOperation, tc.store, now); kerrors.Code(err) != tc.code {
			t.Fatalf("got %v", err)
		}
	}
	if _, err := verifySignature(context.Background(), signedRequest(now.Unix()), "/admin.service.v1.AccessKeyService/Create", nil, now); kerrors.Code(err) != 403 {
		t.Fatal(err)
	}
}
func TestPrincipalRecorderAndActor(t *testing.T) {
	outer := WithIdentityRecorder(context.Background())
	if _, err := PrincipalFromContext(outer); err == nil {
		t.Fatal("unverified identity")
	}
	p := &Principal{Type: SubjectAPIKey, ID: 42, TenantID: 5, Roles: []string{"reader"}}
	inner := NewPrincipalContext(outer, p)
	for _, ctx := range []context.Context{outer, inner} {
		got, err := PrincipalFromContext(ctx)
		if err != nil || got != p {
			t.Fatal("identity not observed")
		}
	}
	if got, err := p.Actor(); err != nil || got != "governance:access-key:42" {
		t.Fatal(got, err)
	}
	p.Type = SubjectUser
	if got, err := p.Actor(); err != nil || got != "governance:user:42" {
		t.Fatal(got, err)
	}
}

func TestSignedHTTPCommonPrincipalAndHeaders(t *testing.T) {
	store := &signingStoreStub{key: &SigningKey{ID: 42, TenantID: 7, RoleAllowed: true, Role: "tenant:reader", Secret: vectorSK}}
	srv := khttp.NewServer(khttp.Middleware(CredentialHeaders(), Server(WithSigningKeyStore(store), WithInjectMetadata(false))))
	calls := 0
	srv.Route("/").GET("/api/v1/networks/vpcs/{vpc_id}", func(c khttp.Context) error {
		khttp.SetOperation(c, VPCReadOperation)
		_, err := c.Middleware(func(ctx context.Context, _ interface{}) (interface{}, error) {
			calls++
			p, err := PrincipalFromContext(ctx)
			if err != nil || p.Type != SubjectAPIKey || p.ID != 42 || p.TenantID != 7 {
				t.Fatal("bad key principal", err)
			}
			claims, err := FromContext(ctx)
			if err != nil || claims.GetUserId() != 0 || claims.GetTenantId() != 7 {
				t.Fatal("key impersonated user")
			}
			v, ok := viewer.FromContext(ctx)
			if !ok || v.IsPlatformContext() || v.IsSystemContext() || v.TenantID() != 7 || v.UserID() != 0 {
				t.Fatal("key received privileged viewer")
			}
			return nil, nil
		})(c, nil)
		if err != nil {
			return err
		}
		return c.JSON(200, map[string]bool{"ok": true})
	})
	for _, tc := range []struct {
		name   string
		code   int
		mutate func(*http.Request)
	}{
		{"valid", 200, func(*http.Request) {}},
		{"mixed", 400, func(r *http.Request) { r.Header.Set("Authorization", "Bearer fake") }},
		{"duplicate", 401, func(r *http.Request) { r.Header.Add("X-Signature", r.Header.Get("X-Signature")) }},
		{"missing", 401, func(r *http.Request) { r.Header.Del("X-Access-Key") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := signedRequest(time.Now().Unix())
			tc.mutate(r)
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, r)
			if rr.Code != tc.code {
				t.Fatalf("HTTP %d expected %d: %s", rr.Code, tc.code, rr.Body.String())
			}
		})
	}
	if calls != 1 {
		t.Fatalf("handler calls %d", calls)
	}
}

func TestRoleRejectionFollowsSignatureVerification(t *testing.T) {
	now := time.Unix(1700000000, 0)
	store := &signingStoreStub{key: &SigningKey{ID: 42, TenantID: 7, Role: "reader", Secret: vectorSK, RoleAllowed: false}}
	bad := signedRequest(now.Unix())
	bad.Header.Set("X-Signature", strings.Repeat("0", 64))
	if p, err := verifySignature(context.Background(), bad, VPCReadOperation, store, now); kerrors.Code(err) != 401 || p != nil {
		t.Fatal("unverified role status exposed", err)
	}
	if store.touched != 0 {
		t.Fatal("invalid signature touched key")
	}
	p, err := verifySignature(context.Background(), signedRequest(now.Unix()), VPCReadOperation, store, now)
	if kerrors.Code(err) != 403 || p == nil || p.ID != 42 || len(p.Roles) != 0 || store.touched != 1 {
		t.Fatal("valid signature did not reach role gate", err)
	}
}
