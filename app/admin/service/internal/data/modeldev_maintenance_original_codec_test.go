//go:build modeldev_maintenance_codec

package data

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"go-wind-admin/app/admin/service/internal/data/ent"
)

// This explicit maintenance-only regression consumes the five original rows
// from a protected, read-only capture. Canonical bytes are never committed or
// logged. The normal persisted-acceptance decoder remains the only decoder.
func TestMaintenanceOriginalFiveAcceptanceCodec(t *testing.T) {
	path := os.Getenv("OWNER_QUALIFICATION_PRIVATE_INPUT")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 16<<20 {
		t.Fatal("explicit private mode0600 original-five input required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("private original-five input unavailable")
	}
	var input struct {
		Governance struct {
			Acceptances []map[string]json.RawMessage `json:"acceptances"`
		} `json:"governance"`
	}
	if json.Unmarshal(raw, &input) != nil {
		t.Fatal("original-five capture shape invalid")
	}
	expected := map[uint32]string{
		55: "cb9831ca80e7df95cf1ca3746db4ff53b623b51671efcf34230275235e0c1690",
		57: "9883ac49d909b2809e78002db7d47d564caa88f84bd29ba14d6248a34e8bb096",
		59: "e75a332cef7521f278df8fc78df0b3f869d8499f96d188fd3b8a915fe02ec829",
		60: "85fa8793e5ff44c8c611316129f344a36124e027f03cc71bf797d89fa1d40a45",
		74: "60df838e3e66d376e7c8c8de51c9ef630aa6aaa6b627823c9867f1da2fdca232",
	}
	if len(input.Governance.Acceptances) != len(expected) {
		t.Fatal("exact original-five scope required")
	}
	seen := make(map[uint32]bool)
	for _, fields := range input.Governance.Acceptances {
		// PostgreSQL's JSON bytea format is hex; Ent's standard JSON format is
		// base64. This changes fixture representation only, never the bytes.
		for _, name := range []string{"intent_canonical", "snapshot_canonical", "owner_receipt_canonical", "close_receipt_canonical"} {
			if string(fields[name]) == "null" || len(fields[name]) == 0 {
				continue
			}
			var encoded string
			if json.Unmarshal(fields[name], &encoded) != nil || !strings.HasPrefix(encoded, "\\x") {
				t.Fatal("original bytea fixture invalid")
			}
			payload, err := hex.DecodeString(strings.TrimPrefix(encoded, "\\x"))
			if err != nil {
				t.Fatal("original bytea fixture invalid")
			}
			fields[name], _ = json.Marshal(payload)
		}
		encoded, _ := json.Marshal(fields)
		var row ent.ModelDevAcceptance
		if json.Unmarshal(encoded, &row) != nil || row.TenantID == nil || *row.TenantID != 2 || row.ResourceTenantID != "64c5c35a-f554-432c-b4bc-7b4f36f08420" || seen[row.ID] {
			t.Fatal("original row identity invalid")
		}
		seen[row.ID] = true
		digest := sha256.Sum256(row.SnapshotCanonical)
		if expected[row.ID] == "" || expected[row.ID] != hex.EncodeToString(digest[:]) || row.ExecutionSpecHash != expected[row.ID] {
			t.Fatal("original snapshot identity differs")
		}
		originalIntent := append([]byte(nil), row.IntentCanonical...)
		originalSnapshot := append([]byte(nil), row.SnapshotCanonical...)
		accepted, err := modelDevAcceptanceFromRow(&row)
		if err != nil {
			t.Errorf("original acceptance %d rejected by pinned native decoder", row.ID)
			continue
		}
		snapshot, err := accepted.Snapshot.Canonical()
		if err != nil || !bytes.Equal(snapshot, originalSnapshot) || !bytes.Equal(row.IntentCanonical, originalIntent) || !bytes.Equal(row.SnapshotCanonical, originalSnapshot) || accepted.ExecutionSpecHash != row.ExecutionSpecHash || accepted.DispatchState != string(row.DispatchState) || accepted.OperationID != row.OperationID || accepted.ExecutionID != row.ExecutionID {
			t.Fatal("native decoding changed original immutable identity or bytes")
		}
		altered := row
		altered.ExecutionSpecHash = strings.Repeat("0", 64)
		if _, err := modelDevAcceptanceFromRow(&altered); err == nil {
			t.Fatal("changed original spec hash accepted")
		}
		t.Logf("original acceptance %d: native decode and byte-identical snapshot PASS; changed-spec refusal PASS", row.ID)
	}
}
