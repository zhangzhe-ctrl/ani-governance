//go:build gpu_joint

package gpucontract

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	attachment "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/integration/v1"
	acc "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"google.golang.org/protobuf/proto"
)

func runCapacityAdmissionCases(t *testing.T, ctx context.Context, client *data.AcceleratorClient, request *acc.GpuRequest, ref *acc.GpuUsageRef, pod *acc.PodRef) {
	t.Helper()
	root := os.Getenv("GOV_ACC_JOINT_DIR")
	directory := filepath.Join(root, "capacity-"+uuid.NewString())
	if e := os.Mkdir(directory, 0700); e != nil {
		t.Fatal(e)
	}
	admin := &acc.AdminRead{RequestId: uuid.NewString(), Actor: &acc.Actor{Type: "user", Id: "1"}}
	tenant := &acc.TenantContext{RequestId: uuid.NewString(), TenantId: ref.TenantId, Actor: admin.Actor}
	// Enumerate and retain the pre-image before advancing controlled fixture
	// observations. Every old binding gets its own positive scoped proof; no
	// business row, usage projection or binding is deleted or guessed.
	devices := []*acc.PhysicalDevice{}
	page := &acc.Page{Size: 200}
	for {
		result, e := client.Admin.ListDevices(ctx, &acc.ListDevicesRequest{Context: admin, ClusterId: request.ClusterId, Page: page})
		if e != nil {
			t.Fatal(e)
		}
		devices = append(devices, result.Items...)
		if result.NextToken == "" {
			break
		}
		page.Token = result.NextToken
	}
	if len(devices) == 0 {
		t.Fatal("capacity fixture has no real persisted devices")
	}
	bindings := []*acc.GpuBinding{}
	page = &acc.Page{Size: 200}
	for {
		result, e := client.Admin.AdminListBindings(ctx, &acc.AdminListBindingsRequest{Context: admin, ClusterId: request.ClusterId, Page: page})
		if e != nil {
			t.Fatal(e)
		}
		bindings = append(bindings, result.Items...)
		if result.NextToken == "" {
			break
		}
		page.Token = result.NextToken
	}
	before, _ := json.MarshalIndent(map[string]any{"devices": devices, "bindings": bindings}, "", "  ")
	if e := os.WriteFile(filepath.Join(directory, "before.json"), before, 0600); e != nil {
		t.Fatal(e)
	}
	serial := 0
	observe := func(value any) {
		serial++
		file := filepath.Join(directory, uuid.NewString()+".json")
		raw, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(file, raw, 0600); e != nil {
			t.Fatal(e)
		}
		command := exec.Command(filepath.Join(root, "acc-contract.test"), "-test.run=^TestJointObservationFixture$", "-test.v")
		command.Env = append(os.Environ(), "ACC_TEST_DSN_FILE="+filepath.Join(root, "acc.dsn"), "ACC_JOINT_OBSERVATION_FILE="+file)
		output, e := command.CombinedOutput()
		if e != nil {
			t.Fatalf("scoped observation %d: %v %s", serial, e, output)
		}
	}
	for _, binding := range bindings {
		if binding.State == acc.AllocationState_RELEASE_CONFIRMED {
			continue
		}
		if binding.VerifiedUsageRef == nil || binding.Pod == nil {
			t.Fatal("fixture scope is not authoritative", binding.BindingId)
		}
		observe(map[string]any{"ref": binding.VerifiedUsageRef, "state": "RELEASED", "device_id": binding.PhysicalDeviceId, "pod": binding.Pod})
	}
	for _, device := range devices {
		for i := 0; i < 4; i++ {
			scoped := proto.Clone(pod).(*acc.PodRef)
			scoped.Uid = "capacity-" + uuid.NewString()
			scoped.Name = "capacity-fixture"
			observe(map[string]any{"ref": ref, "state": "ACTIVE", "device_id": device.DeviceId, "pod": scoped})
		}
	}
	fit, e := client.Catalog.CheckGpuFit(ctx, &acc.CheckGpuFitRequest{Context: tenant, Gpu: request})
	if e != nil || !fit.GetEstimateKnown() || fit.EstimatedReplicas != 0 {
		t.Fatal("known zero capacity fixture failed", fit, e)
	}
	acceptAndCancel := func(name string) {
		var accepted data.QuotaOccupyResult
		if _, e := jointHTTP(ctx, "/create", map[string]any{"Key": uuid.NewString(), "Name": name, "Gpu": request}, &accepted); e != nil {
			t.Fatal(name, e)
		}
		var canceled attachment.GpuDeleteAcceptance
		if _, e := jointHTTP(ctx, "/delete", map[string]string{"Key": uuid.NewString(), "Resource": accepted.ResourceID}, &canceled); e != nil || canceled.Result != "CANCELED_BEFORE_DISPATCH" {
			t.Fatal("waiting acceptance cleanup", canceled, e)
		}
	}
	acceptAndCancel("accepted-at-known-zero-capacity")
	t.Logf("known zero capacity accepted: %d devices, %d scoped fixture transitions", len(devices), serial)
	verifyCapacityBFF(t, directory, "WAIT_FOR_CAPACITY")
	// Let the real configured freshness window elapse. No test clock or unknown
	// override is injected into production readers or business code.
	time.Sleep(62 * time.Second)
	unknown, e := client.Catalog.CheckGpuFit(ctx, &acc.CheckGpuFitRequest{Context: tenant, Gpu: request})
	if e != nil || unknown.GetEstimateKnown() {
		t.Fatal("stale capacity was not unknown", unknown, e)
	}
	acceptAndCancel("accepted-at-unknown-capacity")
	verifyCapacityBFF(t, directory, "FIT_UNKNOWN")
	result, _ := json.MarshalIndent(map[string]any{"zero": fit, "unknown": unknown, "hardware_observed": false, "source": "controlled scoped SaveObservation evolution; real freshness timeout"}, "", "  ")
	if e = os.WriteFile(filepath.Join(directory, "result.json"), result, 0600); e != nil {
		t.Fatal(e)
	}
	t.Log("unknown capacity accepted without inventory reservation")
}

// The optional external verifier runs real JWT/HTTP BFF assertions while the
// joint fixture holds dispatch paused and performs no concurrent ledger writes.
func verifyCapacityBFF(t *testing.T, evidenceDir, expected string) {
	t.Helper()
	script := os.Getenv("GOV_ACC_BFF_CAPACITY_SCRIPT")
	if script == "" {
		return
	}
	root := os.Getenv("GOV_ACC_TASK_ROOT")
	if root == "" {
		t.Fatal("GOV_ACC_TASK_ROOT required for capacity BFF verifier")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "flock", filepath.Join(root, "locks/gov-bff.lock"), "bash", script)
	command.Dir = filepath.Join(root, "gov-bff")
	command.Env = append(os.Environ(), "GOMODCACHE="+filepath.Join(root, "cache/gov-bff-mod"), "GOCACHE="+filepath.Join(root, "cache/gov-bff-build"), "GOV_ACC_BFF_EXPECT_FIT="+expected)
	started := time.Now().UTC()
	output, err := command.CombinedOutput()
	exitCode := 0
	if err != nil {
		exitCode = -1
		if command.ProcessState != nil {
			exitCode = command.ProcessState.ExitCode()
		}
	}
	if e := os.WriteFile(filepath.Join(evidenceDir, "bff-"+expected+".log"), output, 0600); e != nil {
		t.Fatal(e)
	}
	receipt, _ := json.MarshalIndent(map[string]any{"command": []string{"bash", script}, "expected_fit": expected, "started_at": started, "finished_at": time.Now().UTC(), "exit_code": exitCode}, "", "  ")
	if e := os.WriteFile(filepath.Join(evidenceDir, "bff-"+expected+".json"), receipt, 0600); e != nil {
		t.Fatal(e)
	}
	if err != nil {
		t.Fatalf("capacity BFF %s: %v\n%s", expected, err, output)
	}
	t.Logf("real JWT/HTTP BFF capacity %s passed", expected)
}
