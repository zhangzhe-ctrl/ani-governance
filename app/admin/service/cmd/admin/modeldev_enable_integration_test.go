//go:build modeldev_pg

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	modeldevv1 "github.com/zhangzhe-ctrl/ani-modeldev-service/api/ani/modeldev/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type modelDevEnablePeer struct {
	modeldevv1.UnimplementedModelDevManagementServiceServer
	tenant, actor, preset, release, digest string
}

func (p *modelDevEnablePeer) ValidateRelease(ctx context.Context, in *modeldevv1.ValidateReleaseRequest) (*modeldevv1.ValidateReleaseResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	for key, want := range map[string]string{"x-ani-tenant-id": p.tenant, "x-ani-actor": p.actor, "x-ani-authorized-method": modeldevv1.ModelDevManagementService_ValidateRelease_FullMethodName, "x-ani-data-scope": "tenant-all"} {
		if values := md.Get(key); len(values) != 1 || values[0] != want {
			return nil, status.Error(codes.PermissionDenied, "invalid delegation")
		}
	}
	if in.PresetId != p.preset || in.ReleaseId != p.release || in.ReleaseDigest != p.digest {
		return nil, status.Error(codes.NotFound, "release unavailable")
	}
	return &modeldevv1.ValidateReleaseResponse{PresetId: in.PresetId, ReleaseId: in.ReleaseId, ReleaseDigest: in.ReleaseDigest}, nil
}

// The ModelDev peer is the only external boundary substitute. Governance's
// CLI, JWT/Redis sessions, current PG grants, tenant mapping and CAS are real.
func runModelDevEnableVerticalCase(t *testing.T, ctx context.Context, directory, tokenFile, tenant, actor string, scope data.ModelDevReleaseBindingScope, target data.ModelDevReleaseBindingTarget) {
	t.Helper()
	// No default binding exists for this preset: first enable must create the
	// tenant-owned pointer through the same authenticated CAS command.
	scope.PresetID = uuid.NewString()
	config, security := modelDevEnableTLS(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	config.Address = listener.Addr().String()
	peer := &modelDevEnablePeer{tenant: tenant, actor: actor, preset: scope.PresetID, release: target.ReleaseID, digest: target.ReleaseDigest}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(security)))
	modeldevv1.RegisterModelDevManagementServiceServer(server, peer)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	for key, value := range map[string]string{"ANI_MODELDEV_ADDR": config.Address, "ANI_MODELDEV_CA": config.CAFile, "ANI_MODELDEV_CERT": config.CertFile, "ANI_MODELDEV_KEY": config.KeyFile, "ANI_MODELDEV_TIMEOUT": "2s"} {
		t.Setenv(key, value)
	}
	requestFile := filepath.Join(t.TempDir(), "enable.json")
	write := func(release, digest string, generation uint64) {
		raw, err := json.Marshal(map[string]any{"preset_id": scope.PresetID, "release_id": release, "release_digest": digest, "expected_generation": generation, "reason": "owned enable/rollback integration", "evidence_reference": "contract:cpu-p01:enable-rollback"})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(requestFile, raw, 0600))
	}
	args := []string{"modeldev-enable", "--conf", directory, "--token-file", tokenFile, "--request-file", requestFile}
	var output bytes.Buffer
	write(target.ReleaseID, target.ReleaseDigest, 0)
	require.NoError(t, runAdmin(ctx, args, &output))
	first := decodeModelDevPauseOutput(t, output.Bytes())
	require.False(t, first.Replayed)
	require.Equal(t, uint64(1), first.After.Generation)
	var created map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(output.Bytes(), &created))
	require.Equal(t, "null", string(created["before"]))
	write(target.ReleaseID, target.ReleaseDigest, 1)
	output.Reset()
	require.NoError(t, runAdmin(ctx, args, &output))
	require.True(t, decodeModelDevPauseOutput(t, output.Bytes()).Replayed)
	peer.release = uuid.NewString()
	peer.digest = strings.Repeat("b", 64)
	write(peer.release, peer.digest, 1)
	output.Reset()
	require.NoError(t, runAdmin(ctx, args, &output))
	changed := decodeModelDevPauseOutput(t, output.Bytes())
	require.False(t, changed.Replayed)
	require.Equal(t, uint64(2), changed.After.Generation)
	require.Equal(t, target.ReleaseID, changed.Before.ReleaseID)
	require.Equal(t, peer.release, changed.After.ReleaseID)
	// A stale generation cannot change the now-current target back.
	peer.release = target.ReleaseID
	peer.digest = target.ReleaseDigest
	write(peer.release, peer.digest, 1)
	output.Reset()
	require.Error(t, runAdmin(ctx, args, &output))
	require.Empty(t, output.String())
	write(peer.release, peer.digest, 2)
	output.Reset()
	require.NoError(t, runAdmin(ctx, args, &output))
	rolledBack := decodeModelDevPauseOutput(t, output.Bytes())
	require.Equal(t, uint64(3), rolledBack.After.Generation)
	require.Equal(t, target.ReleaseID, rolledBack.After.ReleaseID)
	require.True(t, rolledBack.After.NewSubmissionsEnabled)
	require.Equal(t, actor, rolledBack.Operator)
	t.Log("MODELDEV_ENABLE_CAS PASS: current JWT/session and PG grant, first binding creation, mTLS exact-method validation, target switch, stale-CAS rejection, same-pointer rollback")
}

func modelDevEnableTLS(t *testing.T) (data.ModelDevClientConfig, *tls.Config) {
	t.Helper()
	directory := t.TempDir()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, public, private)
	require.NoError(t, err)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	caFile := filepath.Join(directory, "ca.pem")
	require.NoError(t, os.WriteFile(caFile, caPEM, 0600))
	issue := func(label string, serial int64, usage x509.ExtKeyUsage) (string, string, tls.Certificate) {
		pub, key, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		leaf := &x509.Certificate{SerialNumber: big.NewInt(serial), DNSNames: []string{label}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
		encoded, err := x509.CreateCertificate(rand.Reader, leaf, ca, pub, private)
		require.NoError(t, err)
		keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
		require.NoError(t, err)
		certFile, keyFile := filepath.Join(directory, label+".pem"), filepath.Join(directory, label+".key")
		require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: encoded}), 0600))
		require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}), 0600))
		pair, err := tls.LoadX509KeyPair(certFile, keyFile)
		require.NoError(t, err)
		return certFile, keyFile, pair
	}
	_, _, server := issue("ani-modeldev-service", 2, x509.ExtKeyUsageServerAuth)
	cert, key, _ := issue("ani-governance", 3, x509.ExtKeyUsageClientAuth)
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(caPEM))
	return data.ModelDevClientConfig{CAFile: caFile, CertFile: cert, KeyFile: key, Timeout: 2 * time.Second}, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{server}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert}
}
