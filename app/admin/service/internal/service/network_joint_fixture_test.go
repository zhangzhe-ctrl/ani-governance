//go:build networkintegration

package service

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go-wind-admin/app/admin/service/internal/data"
)

type networkResourcePeer struct {
	Address, CAFile, CertFile, KeyFile, RuntimeDSN                  string
	TenantID, ParentVPC, EntrySubnet, BackendSubnet, BackendAddress string
	Client                                                          *data.NetworkClient
	DB                                                              *sql.DB
}

func newNetworkResourcePeer(t *testing.T) *networkResourcePeer {
	t.Helper()
	binary := os.Getenv("RESOURCE_NETWORK_TEST_BINARY")
	require.NotEmpty(t, binary, "exact Resource test binary required")
	require.True(t, filepath.IsAbs(binary))
	dir := t.TempDir()
	cmd := exec.Command(binary, "-test.run=^TestNetworkGovernanceFixture$", "-test.timeout=8m", "-test.v")
	cmd.Env = append(os.Environ(), "NETWORK_JOINT_FIXTURE_DIR="+dir)
	input, err := cmd.StdinPipe()
	require.NoError(t, err)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	require.NoError(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_, _ = fmt.Fprintln(input, "stop")
		_ = input.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Log(output.String())
			}
			require.NoError(t, err, "Resource fixture product cleanup/isolated DB teardown must pass")
			t.Logf("Resource fixture pid=%d exit=0; product and infrastructure cleanup pass", cmd.Process.Pid)
		case <-time.After(30 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			t.Error("Resource fixture exceeded cleanup deadline")
		}
	})
	var raw []byte
	require.Eventually(t, func() bool { raw, err = os.ReadFile(filepath.Join(dir, "fixture.json")); return err == nil }, 30*time.Second, 20*time.Millisecond, "Resource production fixture startup required")
	peer := &networkResourcePeer{}
	require.NoError(t, json.Unmarshal(raw, peer))
	var closeClient func()
	peer.Client, closeClient, err = data.NewNetworkClient(data.NetworkClientConfig{Address: peer.Address, CAFile: peer.CAFile, CertFile: peer.CertFile, KeyFile: peer.KeyFile, Timeout: 5 * time.Second})
	require.NoError(t, err)
	t.Cleanup(closeClient)
	peer.DB, err = sql.Open("pgx", peer.RuntimeDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.DB.Close() })
	return peer
}
