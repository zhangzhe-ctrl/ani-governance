package data

import (
	"os"
	"testing"
)

func TestModelDevCreateMaintenanceConfigurationIsExplicit(t *testing.T) {
	for _, tc := range []struct {
		value         string
		paused, valid bool
	}{{"", true, true}, {"false", false, true}, {"true", true, true}, {"1", false, false}, {"TRUE", false, false}, {" true", false, false}} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("ANI_MODELDEV_CREATE_PAUSED", tc.value)
			paused, err := ModelDevCreatePausedFromEnv()
			if (err == nil) != tc.valid || paused != tc.paused {
				t.Fatalf("paused=%v err=%v", paused, err)
			}
		})
	}
}

func TestModelDevCreateMaintenanceConfigurationUnsetPauses(t *testing.T) {
	t.Setenv("ANI_MODELDEV_CREATE_PAUSED", "restored by cleanup")
	if err := os.Unsetenv("ANI_MODELDEV_CREATE_PAUSED"); err != nil {
		t.Fatal(err)
	}
	paused, err := ModelDevCreatePausedFromEnv()
	if err != nil || !paused {
		t.Fatalf("unset configuration must pause this maintenance candidate: paused=%v err=%v", paused, err)
	}
}
