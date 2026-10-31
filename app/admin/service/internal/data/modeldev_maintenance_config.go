package data

import (
	"errors"
	"os"
)

// ModelDevCreatePausedFromEnv affects only new ModelDev Create admission and
// publisher claims. This dedicated maintenance candidate pauses by default;
// explicit false resumes Create. Connection and Stop/release settings are independent.
func ModelDevCreatePausedFromEnv() (bool, error) {
	switch os.Getenv("ANI_MODELDEV_CREATE_PAUSED") {
	case "", "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, errors.New("invalid ANI_MODELDEV_CREATE_PAUSED configuration")
	}
}
