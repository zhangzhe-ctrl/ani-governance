package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	authv1 "go-wind-admin/api/gen/go/authentication/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"
	appViewer "go-wind-admin/pkg/entgo/viewer"
	"go-wind-admin/pkg/localdeps/go-crud/viewer"
	conf "go-wind-admin/pkg/localdeps/kratos-bootstrap/api/gen/go/conf/v1"
	"go-wind-admin/pkg/localdeps/kratos-bootstrap/bootstrap"
	bConfig "go-wind-admin/pkg/localdeps/kratos-bootstrap/config"
	bLogger "go-wind-admin/pkg/localdeps/kratos-bootstrap/logger"
	"go-wind-admin/pkg/middleware/auth"
)

// runModelDevPause composes the existing authentication and tenant-scoped data
// adapters. It neither initializes deployment data nor connects to ModelDev.
func runModelDevPause(ctx context.Context, args []string, stdout io.Writer) (result error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	defer func() {
		if err := ctx.Err(); err != nil {
			result = err
		}
	}()
	flags := flag.NewFlagSet("modeldev-pause", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configDir := flags.String("conf", "", "existing Governance configuration directory")
	tokenPath := flags.String("token-file", "", "private file containing an admin access token")
	requestPath := flags.String("request-file", "", "private file containing the fixed release pause request")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *configDir == "" || *tokenPath == "" || *requestPath == "" {
		return errors.New("usage: admin modeldev-pause --conf DIR --token-file PATH --request-file PATH")
	}
	tokenBytes, err := readModelDevPrivateFile(*tokenPath, 16*1024)
	if err != nil {
		return errors.New("modeldev pause requires a nonempty regular token file with mode 0600")
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return errors.New("modeldev pause requires an access token")
	}
	requestBytes, err := readModelDevPrivateFile(*requestPath, 32*1024)
	if err != nil {
		return errors.New("modeldev pause requires a nonempty regular request file with mode 0600")
	}
	request, err := decodeModelDevPauseRequest(requestBytes)
	if err != nil {
		return err
	}
	provider, err := bConfig.NewConfigProvider(*configDir)
	if err != nil {
		return errors.New("modeldev pause configuration unavailable")
	}
	defer func() {
		if err := provider.Close(); err != nil && result == nil {
			result = errors.New("modeldev pause configuration cleanup failed")
		}
	}()
	if err := provider.Load(); err != nil {
		return errors.New("modeldev pause configuration unavailable")
	}
	var cfg conf.Bootstrap
	if err := provider.Scan(&cfg); err != nil {
		return errors.New("modeldev pause configuration invalid")
	}
	dbConfig := cfg.GetData().GetDatabase()
	if dbConfig.GetDriver() != "postgres" || dbConfig.GetSource() == "" || dbConfig.GetMigrate() ||
		cfg.GetData().GetRedis().GetAddr() == "" || cfg.GetAuthn().GetType() != "jwt" || cfg.GetAuthn().GetJwt() == nil {
		return errors.New("modeldev pause requires PostgreSQL without automatic migration, Redis and JWT configuration")
	}
	// The command emits a bounded result; secret-bearing adapter errors are not
	// printed. The binding itself retains the trusted actor and change evidence.
	bctx := bootstrap.NewContextWithParam(ctx, nil, &cfg, bLogger.NopLogger())
	db, closeDB, err := data.NewEntClient(bctx)
	if err != nil {
		return errors.New("modeldev pause database unavailable")
	}
	defer closeDB()
	if err := db.DB().PingContext(ctx); err != nil {
		return errors.New("modeldev pause database unavailable")
	}
	rdb, closeRedis, err := data.NewRedisClient(bctx)
	if err != nil {
		return errors.New("modeldev pause session store unavailable")
	}
	defer closeRedis()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return errors.New("modeldev pause session store unavailable")
	}
	authenticator, err := newModelDevPauseAuthenticator(bctx, data.NewUserTokenCache(bctx, rdb))
	if err != nil {
		return err
	}
	verified, err := authenticator.Authenticate(ctx, &authv1.ValidateTokenRequest{
		ClientType: authv1.ClientType_admin, TokenCategory: authv1.TokenCategory_ACCESS, Token: token,
	})
	if err != nil || verified == nil || !verified.IsValid || verified.Payload.GetUserId() == 0 || verified.Payload.GetTenantId() == 0 {
		return errors.New("modeldev pause requires a valid tenant user access token and session")
	}
	principal := &auth.Principal{Type: auth.SubjectUser, ID: verified.Payload.GetUserId(), TenantID: verified.Payload.GetTenantId()}
	ctx = auth.NewPrincipalContext(ctx, principal)
	// Keep the ordinary tenant viewer. Current data scope and action grants are
	// checked from the database in the use case, never inferred from JWT roles.
	ctx = viewer.WithContext(ctx, appViewer.NewUserViewer(uint64(principal.ID), uint64(principal.TenantID), 0, "", nil))
	usecase := service.NewModelDevBindingService(data.NewModelDevAuthorizationRepo(db), data.NewTenantRepo(bctx, db), data.NewModelDevReleaseBindingRepo(db))
	change, err := usecase.Pause(ctx, request)
	if err != nil {
		return err
	}
	if change == nil || change.Before == nil || change.After == nil {
		return errors.New("modeldev pause result unavailable")
	}
	actor, err := principal.Actor()
	if err != nil {
		return errors.New("modeldev pause operator unavailable")
	}
	return json.NewEncoder(stdout).Encode(modelDevPauseResult{
		Operator: actor, Before: modelDevPauseView(change.Before), After: modelDevPauseView(change.After), Replayed: change.Replayed,
	})
}

// The shared authenticator reports invalid startup configuration by panicking.
// Bound that constructor only, without exposing keys or recovering use-case bugs.
func newModelDevPauseAuthenticator(ctx *bootstrap.Context, cache *data.UserTokenCache) (value *data.Authenticator, err error) {
	defer func() {
		if recover() != nil {
			value, err = nil, errors.New("modeldev pause JWT configuration invalid")
		}
	}()
	return data.NewAuthenticator(ctx, cache), nil
}

func readModelDevPrivateFile(path string, limit int64) ([]byte, error) {
	invalid := errors.New("invalid private file")
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm() != 0600 || before.Size() == 0 || before.Size() > limit {
		return nil, invalid
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, invalid
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) || !opened.Mode().IsRegular() || opened.Mode().Perm() != 0600 {
		return nil, invalid
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || len(raw) == 0 || int64(len(raw)) > limit {
		return nil, invalid
	}
	return raw, nil
}

// Decode only the six fixed fields. Reject duplicate, unknown, null and trailing
// input so a request cannot silently supply another actor, tenant or gate.
func decodeModelDevPauseRequest(raw []byte) (service.ModelDevPauseInput, error) {
	var result service.ModelDevPauseInput
	invalid := errors.New("invalid modeldev pause request")
	decoder := json.NewDecoder(bytes.NewReader(raw))
	first, err := decoder.Token()
	if !utf8.Valid(raw) || err != nil || first != json.Delim('{') {
		return result, invalid
	}
	seen := make(map[string]bool, 6)
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return result, invalid
		}
		seen[name] = true
		var field json.RawMessage
		if err := decoder.Decode(&field); err != nil || bytes.Equal(bytes.TrimSpace(field), []byte("null")) {
			return result, invalid
		}
		var target any
		switch name {
		case "preset_id":
			target = &result.PresetID
		case "release_id":
			target = &result.ReleaseID
		case "release_digest":
			target = &result.ReleaseDigest
		case "expected_generation":
			target = &result.ExpectedGeneration
		case "reason":
			target = &result.Reason
		case "evidence_reference":
			target = &result.EvidenceReference
		default:
			return result, invalid
		}
		if err := json.Unmarshal(field, target); err != nil {
			return result, invalid
		}
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') || len(seen) != 6 || decoder.Decode(new(any)) != io.EOF {
		return result, invalid
	}
	return result, nil
}

type modelDevPauseBindingView struct {
	ReleaseID             string `json:"release_id"`
	ReleaseDigest         string `json:"release_digest"`
	Generation            uint64 `json:"generation"`
	NewSubmissionsEnabled bool   `json:"new_submissions_enabled"`
	UpdatedBy             string `json:"updated_by"`
}

type modelDevPauseResult struct {
	Operator string                   `json:"operator"`
	Before   modelDevPauseBindingView `json:"before"`
	After    modelDevPauseBindingView `json:"after"`
	Replayed bool                     `json:"replayed"`
}

func modelDevPauseView(binding *data.ModelDevReleaseBinding) modelDevPauseBindingView {
	return modelDevPauseBindingView{
		ReleaseID: binding.Target.ReleaseID, ReleaseDigest: binding.Target.ReleaseDigest,
		Generation: binding.Generation, NewSubmissionsEnabled: binding.Target.NewSubmissionsEnabled, UpdatedBy: binding.UpdatedBy,
	}
}
