package data

import (
	"testing"
	"time"
)

func TestModelDevConfigFromEnvDisablesUnconfiguredResolution(t *testing.T) {
	clearModelDevConfigEnvironment(t)
	got, err := ModelDevConfigFromEnv()
	if err != nil || got != (ModelDevClientConfig{}) {
		t.Fatalf("unconfigured resolution must be disabled without an error: config=%+v err=%v", got, err)
	}
}

func TestModelDevConfigFromEnvLoadsExplicitConnection(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout string
		want    time.Duration
	}{
		{name: "default timeout", want: 3 * time.Second},
		{name: "explicit timeout", timeout: "1250ms", want: 1250 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearModelDevConfigEnvironment(t)
			setModelDevConfigConnection(t)
			t.Setenv("ANI_MODELDEV_TIMEOUT", tc.timeout)
			got, err := ModelDevConfigFromEnv()
			want := ModelDevClientConfig{
				Address:  "modeldev.internal:9443",
				CAFile:   "/managed/modeldev/ca.pem",
				CertFile: "/managed/modeldev/client.pem",
				KeyFile:  "/managed/modeldev/client.key",
				Timeout:  tc.want,
			}
			if err != nil || got != want {
				t.Fatalf("explicit connection settings must be loaded without reading certificate files: got=%+v want=%+v err=%v", got, want, err)
			}
		})
	}
}

func TestModelDevConfigFromEnvRejectsPartialConnection(t *testing.T) {
	for _, missing := range []string{"ANI_MODELDEV_ADDR", "ANI_MODELDEV_CA", "ANI_MODELDEV_CERT", "ANI_MODELDEV_KEY"} {
		t.Run(missing, func(t *testing.T) {
			clearModelDevConfigEnvironment(t)
			setModelDevConfigConnection(t)
			t.Setenv(missing, "")
			got, err := ModelDevConfigFromEnv()
			if err == nil || got != (ModelDevClientConfig{}) {
				t.Fatalf("partial configuration must fail without a usable client configuration: got=%+v err=%v", got, err)
			}
		})
	}
}

func TestModelDevConfigFromEnvRejectsInvalidTimeout(t *testing.T) {
	for _, timeout := range []string{"not-a-duration", "0s", "-1s"} {
		t.Run(timeout, func(t *testing.T) {
			clearModelDevConfigEnvironment(t)
			setModelDevConfigConnection(t)
			t.Setenv("ANI_MODELDEV_TIMEOUT", timeout)
			got, err := ModelDevConfigFromEnv()
			if err == nil || got != (ModelDevClientConfig{}) {
				t.Fatalf("invalid timeout must fail without a usable client configuration: got=%+v err=%v", got, err)
			}
		})
	}
}

func clearModelDevConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"ANI_MODELDEV_ADDR", "ANI_MODELDEV_CA", "ANI_MODELDEV_CERT", "ANI_MODELDEV_KEY", "ANI_MODELDEV_TIMEOUT"} {
		t.Setenv(name, "")
	}
}

func setModelDevConfigConnection(t *testing.T) {
	t.Helper()
	t.Setenv("ANI_MODELDEV_ADDR", "modeldev.internal:9443")
	t.Setenv("ANI_MODELDEV_CA", "/managed/modeldev/ca.pem")
	t.Setenv("ANI_MODELDEV_CERT", "/managed/modeldev/client.pem")
	t.Setenv("ANI_MODELDEV_KEY", "/managed/modeldev/client.key")
}
