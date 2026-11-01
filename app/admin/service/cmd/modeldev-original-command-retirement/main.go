// Command modeldev-original-command-retirement is a finite operator tool. It is
// deliberately absent from the server/admin runtime and never resumes a queue.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent"
	_ "go-wind-admin/app/admin/service/internal/data/ent/runtime"
	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"
)

type configuration struct {
	GovernanceDSNFile, ModelDevReadOnlyDSNFile string
	ModelDev                                   data.ModelDevClientConfig
}

func readPrivate(path string, limit int64) ([]byte, error) {
	i, e := os.Lstat(path)
	if e != nil || !i.Mode().IsRegular() || i.Mode().Perm() != 0600 || i.Size() == 0 || i.Size() > limit {
		return nil, errors.New("PRIVATE_FILE_REQUIRED")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, errors.New("PRIVATE_FILE_UNAVAILABLE")
	}
	defer f.Close()
	j, e := f.Stat()
	if e != nil || !os.SameFile(i, j) {
		return nil, errors.New("PRIVATE_FILE_CHANGED")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, errors.New("PRIVATE_FILE_UNAVAILABLE")
	}
	return b, nil
}
func decode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("STRICT_JSON_REQUIRED")
	}
	return nil
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func openDatabase(path string, readonly bool) (*sql.DB, error) {
	b, e := readPrivate(path, 1<<20)
	if e != nil {
		return nil, e
	}
	c, e := pgx.ParseConfig(string(bytes.TrimSpace(b)))
	if e != nil {
		return nil, errors.New("DATABASE_CONFIGURATION_INVALID")
	}
	if c.RuntimeParams == nil {
		c.RuntimeParams = map[string]string{}
	}
	if readonly {
		c.RuntimeParams["default_transaction_read_only"] = "on"
	}
	c.RuntimeParams["statement_timeout"] = "15000"
	db := stdlib.OpenDB(*c)
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	return db, nil
}
func run() error {
	mode := flag.String("mode", "plan", "plan (read only) or apply")
	phase := flag.String("phase", "", "apply: stop or deliver; each requires a newly read approved plan")
	id := flag.Uint("id", 0, "one of original acceptance IDs 55,57,59,60")
	configPath := flag.String("config", "", "private0600 configuration")
	planPath := flag.String("plan", "", "canonical plan JSON from this command")
	planSHA := flag.String("plan-sha256", "", "root-approved exact plan bytes SHA256")
	proofPath := flag.String("proof", "", "fresh root identity/503 proof JSON")
	proofSHA := flag.String("proof-sha256", "", "root-approved exact proof bytes SHA256")
	sourcePath := flag.String("sourceproof", "", "root-reviewed original evidence bytes")
	flag.Parse()
	if *mode != "plan" && *mode != "apply" {
		return errors.New("MODE_INVALID")
	}
	if *mode == "apply" && (*id > uint(^uint32(0)) || (*phase != "stop" && *phase != "deliver")) {
		return errors.New("APPLY_PHASE_OR_ID_INVALID")
	}
	b, e := readPrivate(*configPath, 1<<20)
	if e != nil {
		return e
	}
	configurationSHA := digest(b)
	var cfg configuration
	if e = decode(b, &cfg); e != nil {
		return e
	}
	db, e := openDatabase(cfg.GovernanceDSNFile, *mode == "plan")
	if e != nil {
		return e
	}
	driver := entsql.OpenDB("postgres", db)
	ec := ent.NewClient(ent.Driver(driver))
	defer ec.Close()
	repo := data.NewModelDevAcceptanceRepo(entCrud.NewEntClient(ec, driver))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if *mode == "plan" {
		p, e := repo.PlanOriginalCommandRetirement(ctx)
		if e != nil {
			return errors.New("ORIGINAL_PLAN_READ_FAILED")
		}
		md, e := openDatabase(cfg.ModelDevReadOnlyDSNFile, true)
		if e != nil {
			return e
		}
		defer md.Close()
		audit, e := repo.AuditOriginalCommandCodecs(ctx, md)
		if e != nil {
			return e
		}
		p.CodecAudit = &audit
		b, e = json.Marshal(p)
		if e != nil {
			return e
		}
		if _, e = os.Stdout.Write(b); e != nil {
			return e
		}
		fmt.Fprintln(os.Stderr, "PLAN_SHA256", digest(b))
		return nil
	}
	b, e = readPrivate(*planPath, 2<<20)
	if e != nil {
		return e
	}
	var plan data.OriginalCommandPlan
	if digest(b) != *planSHA || decode(b, &plan) != nil || data.OriginalCommandPlanSHA(plan) != *planSHA {
		return errors.New("EXACT_APPROVED_PLAN_REQUIRED")
	}
	b, e = readPrivate(*proofPath, 1<<20)
	if e != nil {
		return e
	}
	var proof data.OriginalCommandProof
	if digest(b) != *proofSHA || decode(b, &proof) != nil {
		return errors.New("EXACT_ROOT_PROOF_REQUIRED")
	}
	if proof.ConfigurationSHA256 != configurationSHA {
		return errors.New("EXACT_APPROVED_CONFIGURATION_REQUIRED")
	}
	if e = proof.Validate(*planSHA); e != nil {
		return e
	}
	source, e := readPrivate(*sourcePath, 4<<20)
	if e != nil || digest(source) != proof.SourceProofSHA256 {
		return errors.New("ROOT_SOURCEPROOF_BYTES_REQUIRED")
	}
	paused, e := data.ModelDevCreatePausedFromEnv()
	if e != nil || !paused {
		return errors.New("MAINTENANCE_TOOL_MUST_REMAIN_PAUSED")
	}
	if cfg.ModelDev.Timeout <= 0 || cfg.ModelDev.Timeout > 20*time.Second {
		return errors.New("BOUNDED_ORIGINAL_SENDER_TIMEOUT_REQUIRED")
	}
	sender, closeSender, e := data.NewModelDevClient(cfg.ModelDev)
	if e != nil {
		return errors.New("ORIGINAL_MTLS_SENDER_UNAVAILABLE")
	}
	defer closeSender()
	var receipt any
	if *phase == "stop" {
		receipt, e = repo.StopOriginalCommand(ctx, plan, uint32(*id), proof, sender)
	} else {
		receipt, e = repo.DeliverOriginalCommand(ctx, plan, uint32(*id), proof, sender)
	}
	if e != nil {
		return fmt.Errorf("ORIGINAL_PROTOCOL_ATTEMPT_UNRESOLVED_READ_FRESH_PLAN: %s", data.OriginalCommandFailureReason(e))
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Phase   string
		ID      uint
		Receipt any
	}{*phase, *id, receipt})
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
