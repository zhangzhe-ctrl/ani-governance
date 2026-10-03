package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestModelDevPauseConfigErrorsDoNotExposeSecrets(t *testing.T) {
	const childFlag = "CPU_P01_MODELDEV_PAUSE_CONFIG_CHILD"
	const childDirectory = "CPU_P01_MODELDEV_PAUSE_CONFIG_DIRECTORY"
	if os.Getenv(childFlag) == "1" {
		// Only the isolated child changes argv and enters the production main.
		// The parent never changes its logger, argv or environment.
		private := os.Getenv(childDirectory)
		os.Args = []string{
			os.Args[0], "modeldev-pause",
			"--conf", filepath.Join(private, "config"),
			"--token-file", filepath.Join(private, "token"),
			"--request-file", filepath.Join(private, "request.json"),
		}
		main()
		return
	}

	private := t.TempDir()
	require.NoError(t, os.Chmod(private, 0700))
	configDirectory := filepath.Join(private, "config")
	require.NoError(t, os.Mkdir(configDirectory, 0700))
	// Authentication is never reached: the valid file/request preparation must
	// reach configuration decoding, which fails before PG or Redis is opened.
	require.NoError(t, os.WriteFile(filepath.Join(private, "token"), []byte("synthetic-access-token"), 0600))
	request := []byte(`{
  "preset_id":"11111111-1111-4111-8111-111111111111",
  "release_id":"22222222-2222-4222-8222-222222222222",
  "release_digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "expected_generation":1,
  "reason":"synthetic configuration failure check",
  "evidence_reference":"contract:cpu-p01:pause-config"
}`)
	require.NoError(t, os.WriteFile(filepath.Join(private, "request.json"), request, 0600))
	const secretMarker = "CPU_P01_FICTITIOUS_CONFIG_SECRET_MUST_NOT_BE_LOGGED"
	// Deliberately truncated JSON retains a fictitious secret in the original
	// source bytes. The pinned config reader logged those bytes on decode error.
	badConfig := []byte(`{"authn":{"jwt":{"key":"` + secretMarker + `"}},`)
	require.NoError(t, os.WriteFile(filepath.Join(configDirectory, "bootstrap.json"), badConfig, 0600))

	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestModelDevPauseConfigErrorsDoNotExposeSecrets$", "-test.count=1")
	cmd.Env = append(os.Environ(), childFlag+"=1", childDirectory+"="+private)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	require.NoError(t, ctx.Err(), "the child must finish instead of reaching the test deadline")
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit, "the real main must reject the malformed configuration")
	require.Equal(t, 1, exit.ExitCode())
	require.Contains(t, stderr.String(), "modeldev pause configuration unavailable",
		"a file, request or startup failure is not the intended configuration failure")
	require.NotContains(t, stdout.String(), secretMarker, "MODELDEV_PAUSE_CONFIG_LEAK: configuration bytes reached stdout")
	require.NotContains(t, stderr.String(), secretMarker, "MODELDEV_PAUSE_CONFIG_LEAK: configuration bytes reached stderr")
	require.Empty(t, stdout.String(), "a rejected command must not emit a success result")
	require.Equal(t, "modeldev pause configuration unavailable\n", stderr.String())
}
