//go:build imageintegration

package service

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go-wind-admin/app/admin/service/internal/data"
)

// This exact-SHA test binary is built in the Resource repository. Its server
// uses Resource's real lifecycle, PG migrations/runtime role and mTLS identity
// policy; only Harbor is a provider fixture. Missing inputs fail this gate.
func newImageResourceContractPeer(t *testing.T) *data.ImageClient {
	t.Helper()
	binary := os.Getenv("IMAGE_RESOURCE_CONTRACT_BINARY")
	require.NotEmpty(t, binary, "exact-SHA Resource test binary required")
	require.True(t, filepath.IsAbs(binary))
	ready := filepath.Join(t.TempDir(), "endpoint.json")
	cmd := exec.Command(binary, "-test.run=^TestImageResourceContractHelper$", "-test.timeout=3m")
	cmd.Env = append(os.Environ(), "IMAGE_RESOURCE_CONTRACT_HELPER=1", "IMAGE_RESOURCE_CONTRACT_READY="+ready, "IMAGE_TEST_ADMIN_DSN_FILE="+os.Getenv("IMAGE_GOV_ADMIN_DSN_FILE"))
	input, err := cmd.StdinPipe()
	require.NoError(t, err)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	require.NoError(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = input.Close()
		select {
		case err := <-done:
			require.NoError(t, err, "Resource helper exited unsuccessfully; output excluded from credential logs")
			t.Logf("Resource contract helper pid=%d exit=0; owned database cleanup complete", cmd.Process.Pid)
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			t.Error("owned Resource helper exceeded cleanup deadline")
		}
	})
	var raw []byte
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		raw, err = os.ReadFile(ready)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.NoError(t, err, "Resource contract server did not become ready")
	var cfg data.ImageClientConfig
	require.NoError(t, json.Unmarshal(raw, &cfg))
	cfg.Timeout = 5 * time.Second
	client, closeClient, err := data.NewImageClient(cfg)
	require.NoError(t, err)
	t.Cleanup(closeClient)
	return client
}
