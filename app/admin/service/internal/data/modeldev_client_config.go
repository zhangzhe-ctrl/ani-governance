package data

import "errors"

// ModelDevConfigFromEnv reads only the explicit Governance-to-ModelDev
// connection settings. Empty configuration disables new remote resolution;
// accepted-key lookup does not require this client. Certificate material and
// the fixed server identity are validated by NewModelDevClient.
func ModelDevConfigFromEnv() (ModelDevClientConfig, error) {
	return ModelDevClientConfig{}, errors.New("modeldev environment configuration not implemented")
}
