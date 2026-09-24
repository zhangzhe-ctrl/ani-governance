package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	acc "github.com/zhangzhe-ctrl/ani-accelerator-service/api/gen/go/accelerator/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func gpuVector(t *testing.T) (*acc.ResolvedGpuPlan, *acc.GpuUsageProjection, *acc.ProviderBaseline) {
	t.Helper()
	raw, err := os.ReadFile("testdata/accelerator-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]json.RawMessage
	if err = json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	p := &acc.ResolvedGpuPlan{}
	projection := &acc.GpuUsageProjection{}
	baseline := &acc.ProviderBaseline{}
	for key, msg := range map[string]proto.Message{"plan": p, "projection": projection, "baseline": baseline} {
		if err = protojson.Unmarshal(v[key], msg); err != nil {
			t.Fatal(err)
		}
	}
	return p, projection, baseline
}

func TestGpuFrozenCanonicalVectors(t *testing.T) {
	p, projection, b := gpuVector(t)
	if d, e := GpuPlanDigest(p); e != nil || d != p.ResolutionDigest {
		t.Fatalf("plan %s %v", d, e)
	}
	if d, e := GpuProjectionDigest(projection); e != nil || d != projection.PayloadDigest {
		t.Fatalf("projection %s %v", d, e)
	}
	want := b.BaselineDigest
	b.BaselineDigest = ""
	b.Verification = nil
	b.FactorEvidenceRef = ""
	v, e := gpuCanonicalMessage(b.ProtoReflect())
	if e != nil {
		t.Fatal(e)
	}
	if d, e := gpuHash(v); e != nil || d != want {
		t.Fatalf("baseline %s %v", d, e)
	}
	if d, e := gpuSpecDigest(p.Profile); e != nil || d != p.Profile.SpecDigest {
		t.Fatalf("spec %s %v", d, e)
	}
	if q, e := GpuPlanQuota(p); e != nil || q.QuotaCode != GpuSharedQuotaCode || q.Units != 12288 {
		t.Fatalf("quota %+v %v", q, e)
	}
	// Published and KeyValue ordering do not change the immutable plan digest.
	p.Profile.Published = false
	p.Runtime.NodeLabels[0], p.Runtime.NodeLabels[2] = p.Runtime.NodeLabels[2], p.Runtime.NodeLabels[0]
	if d, e := GpuPlanDigest(p); e != nil || d != p.ResolutionDigest {
		t.Fatal(d, e)
	}
	p.Runtime.NodeLabels = append(p.Runtime.NodeLabels, p.Runtime.NodeLabels[0])
	if _, e := GpuPlanDigest(p); e == nil {
		t.Fatal("duplicate managed key accepted")
	}
}

func TestGpuCanonicalRejectsSemanticMutations(t *testing.T) {
	original, _, _ := gpuVector(t)
	mutations := map[string]func(*acc.ResolvedGpuPlan){
		"factor":        func(p *acc.ResolvedGpuPlan) { p.Encoding.MemoryBlockMib = 10 },
		"double-charge": func(p *acc.ResolvedGpuPlan) { p.Totals.ExclusiveDeviceCount = 2 },
		"replicas":      func(p *acc.ResolvedGpuPlan) { p.Request.Replicas = 17 },
		"extra-gpu":     func(p *acc.ResolvedGpuPlan) { p.Request.DevicesPerReplica = 2 },
		"runtime":       func(p *acc.ResolvedGpuPlan) { p.Runtime.SchedulerName = "default-scheduler" },
		"overflow":      func(p *acc.ResolvedGpuPlan) { p.Encoding.MemoryBlocksPerDevice = 9223372036854775807 },
		"negative":      func(p *acc.ResolvedGpuPlan) { p.Totals.SharedMemoryMib = -1 },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			p := proto.Clone(original).(*acc.ResolvedGpuPlan)
			mutate(p)
			if d, e := GpuPlanDigest(p); e == nil {
				p.ResolutionDigest = d
			}
			if _, e := GpuPlanQuota(p); e == nil {
				t.Fatal("invalid plan accepted despite matching digest")
			}
		})
	}
	for _, factor := range []uint32{1, 256, 1024} {
		p := proto.Clone(original).(*acc.ResolvedGpuPlan)
		p.Encoding.MemoryBlockMib = factor
		p.Encoding.MemoryBlocksPerDevice = 6144 / int64(factor)
		for _, v := range p.Runtime.LimitsPerContainer {
			if v.Key == "volcano.sh/vgpu-memory" {
				switch factor {
				case 1:
					v.Value = "6144"
				case 256:
					v.Value = "24"
				case 1024:
					v.Value = "6"
				}
			}
		}
		p.ResolutionDigest, _ = GpuPlanDigest(p)
		if q, e := GpuPlanQuota(p); e != nil || q.Units != 12288 {
			t.Fatal(factor, q, e)
		}
	}
}

func TestGpuCanonicalUnicodeNullInteger(t *testing.T) {
	v := map[string]any{"z": "9223372036854775807", "a": "<&中文>", "b": true}
	b, e := gpuCanonicalJSON(v)
	if e != nil || string(b) != `{"a":"<&中文>","b":true,"z":"9223372036854775807"}` {
		t.Fatal(string(b), e)
	}
	if d, e := gpuHash(v); e != nil || d != "c8151fcfc0f56c9a06c7a35c22ad29a17501bceacabde76c420bff9ffc4a43cb" {
		t.Fatal(d, e)
	}
	p := &acc.ResolvedGpuPlan{}
	obj, e := gpuCanonicalMessage(p.ProtoReflect())
	if e != nil {
		t.Fatal(e)
	}
	b, e = gpuCanonicalJSON(obj)
	if e != nil || !strings.Contains(string(b), `"profile":null`) {
		t.Fatal(string(b), e)
	}
	p.Request = &acc.GpuRequest{ProfileVersion: ^uint64(0)}
	if _, e = gpuCanonicalMessage(p.ProtoReflect()); e == nil {
		t.Fatal("overflow accepted")
	}
}

func TestGpuCanonicalRejectsFloatMessages(t *testing.T) {
	for _, message := range []proto.Message{wrapperspb.Float(0), wrapperspb.Float(1.5), wrapperspb.Double(1), wrapperspb.Double(1.5), wrapperspb.Double(math.NaN()), wrapperspb.Double(math.Inf(1))} {
		if _, err := gpuCanonicalMessage(message.ProtoReflect()); err == nil {
			t.Fatalf("floating protobuf field accepted: %T", message)
		}
	}
}

func TestGpuCanonicalDiffersFromDirectProtoJSONHash(t *testing.T) {
	plan, _, _ := gpuVector(t)
	expected := plan.ResolutionDigest
	plan.ResolutionDigest = ""
	plan.Profile.Published = false
	for _, options := range []protojson.MarshalOptions{{}, {UseProtoNames: true, EmitUnpopulated: true}, {UseProtoNames: true, EmitUnpopulated: true, UseEnumNumbers: true}} {
		raw, err := options.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(append([]byte("acc-c14n-v1\n"), raw...))
		if hex.EncodeToString(sum[:]) == expected {
			t.Fatal("direct protojson hash unexpectedly matched the frozen canonical vector")
		}
	}
	if digest, err := GpuPlanDigest(plan); err != nil || digest != expected {
		t.Fatalf("domain canonical vector changed: %s %v", digest, err)
	}
}

func TestQuotaDurableAckFields(t *testing.T) {
	cmd := &QuotaDispatchCommand{OperationID: "op", ResourceID: "resource"}
	for _, raw := range []string{`{}`, `{"operation_id":"other","resource_id":"resource","accepted":true}`, `{"operation_id":"op","resource_id":"other","accepted":true}`, `{"operation_id":"op","resource_id":"resource","accepted":false}`, `{"operation_id":"other","operation_id":"op","resource_id":"resource","accepted":true}`, `{"operation_id":"op","resource_id":"resource","accepted":true,"Accepted":false}`, `{"operation_id":"op","resource_id":"resource","accepted":true} {}`, `null`, `bad`} {
		if ValidateDurableOwnerAck(cmd, []byte(raw)) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if e := ValidateDurableOwnerAck(cmd, []byte(`{"operation_id":"op","resource_id":"resource","accepted":true}`)); e != nil {
		t.Fatal(e)
	}
}

func TestGpuChargesCompleteAndOrderIndependent(t *testing.T) {
	c := &GpuCanonical{QuotaItems: []GpuQuotaItem{{QuotaCode: GpuSharedQuotaCode, Units: 12288}, {QuotaCode: "storage.bytes", Units: 10}}}
	valid := []QuotaChargeRef{{ChargeID: "storage", QuotaCode: "storage.bytes", ChargedUnits: 10}, {ChargeID: "gpu", QuotaCode: GpuSharedQuotaCode, ChargedUnits: 12288}}
	if refs, e := GpuChargeSubset(c, valid); e != nil || len(refs) != 1 || refs[0].ChargeId != "gpu" {
		t.Fatal(refs, e)
	}
	for _, invalid := range [][]QuotaChargeRef{nil, valid[:1], {valid[0], valid[0]}, {valid[1], {ChargeID: "gpu", QuotaCode: "storage.bytes", ChargedUnits: 10}}, {valid[0], {ChargeID: "gpu", QuotaCode: GpuSharedQuotaCode, ChargedUnits: 12}}} {
		if _, e := GpuChargeSubset(c, invalid); e == nil {
			t.Fatal("invalid original vector accepted", invalid)
		}
	}
}
