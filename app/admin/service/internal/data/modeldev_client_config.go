package data

import (
	"errors"
	"os"
	"strings"
	"time"
)

// ModelDevConfigFromEnv reads only the explicit Governance-to-ModelDev
// connection settings. Empty configuration disables new remote resolution;
// accepted-key lookup does not require this client. Certificate material and
// the fixed server identity are validated by NewModelDevClient.
func ModelDevConfigFromEnv() (ModelDevClientConfig, error) {
	config := ModelDevClientConfig{
		Address: os.Getenv("ANI_MODELDEV_ADDR"),
		CAFile: os.Getenv("ANI_MODELDEV_CA"),
		CertFile: os.Getenv("ANI_MODELDEV_CERT"),
		KeyFile: os.Getenv("ANI_MODELDEV_KEY"),
	}
	timeout := os.Getenv("ANI_MODELDEV_TIMEOUT")
	if config == (ModelDevClientConfig{}) && timeout == "" {
		return ModelDevClientConfig{}, nil
	}
	invalid := errors.New("invalid modeldev environment configuration")
	for _, value := range []string{config.Address, config.CAFile, config.CertFile, config.KeyFile} {
		if value == "" || strings.TrimSpace(value) != value {
			return ModelDevClientConfig{}, invalid
		}
	}
	config.Timeout = 3 * time.Second
	if timeout != "" {
		parsed, err := time.ParseDuration(timeout)
		if err != nil || parsed <= 0 {
			return ModelDevClientConfig{}, invalid
		}
		config.Timeout = parsed
	}
	return config, nil
}
