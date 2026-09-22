package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	stderrors "errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/errors"
)

const VPCReadOperation = "/admin.service.v1.NetworkService/GetVPC"

// Key-enabled operations are explicit. Adding a route never grants machine access implicitly.
var keyOperations = map[string]bool{VPCReadOperation: true}

func allowsAPIKey(operation string) bool { return keyOperations[operation] }

const emptyBodySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

var ErrSigningKeyRejected = stderrors.New("signing key rejected")

type SigningKey struct {
	ID, TenantID uint32
	Role, Secret string
	RoleAllowed  bool
}
type SigningKeyStore interface {
	LookupSigningKey(context.Context, string) (*SigningKey, error)
	MarkSigningKeyUsed(context.Context, uint32) error
}

var accessKeyPattern = regexp.MustCompile(`^ak-[A-Za-z0-9_-]+$`)
var timestampPattern = regexp.MustCompile(`^[1-9][0-9]*$`)
var signaturePattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var vpcPathPattern = regexp.MustCompile(`^/api/v1/networks/vpcs/vpc_[0-9a-f]{32}$`)

func CanonicalVPCRequest(path, accessKey, timestamp string) string {
	return strings.Join([]string{"ANI-HMAC-SHA256", "GET", path, "", accessKey, timestamp, emptyBodySHA256}, "\n")
}
func VPCSignature(secret, path, accessKey, timestamp string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(CanonicalVPCRequest(path, accessKey, timestamp)))
	return hex.EncodeToString(mac.Sum(nil))
}
func hasSigningHeaders(r *http.Request) bool {
	return len(r.Header.Values("X-Access-Key"))+len(r.Header.Values("X-Signature"))+len(r.Header.Values("X-Timestamp")) > 0
}

// ValidateVPCReadRequest applies the same strict read-only contract to both credentials.
func ValidateVPCReadRequest(r *http.Request) error {
	if r.Method != "GET" || !vpcPathPattern.MatchString(r.URL.Path) || r.URL.RawPath != "" || strings.Contains(r.RequestURI, "%") || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return errors.BadRequest("INVALID_VPC_REQUEST", "VPC detail requires its canonical path and no query")
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) > 0 {
		return errors.BadRequest("INVALID_VPC_REQUEST", "VPC detail accepts no body")
	}
	if r.Body != nil {
		b := make([]byte, 1)
		n, err := r.Body.Read(b)
		if n > 0 || (err != nil && err != io.EOF) {
			return errors.BadRequest("INVALID_VPC_REQUEST", "VPC detail accepts no body")
		}
	}
	return nil
}
func verifySignature(ctx context.Context, r *http.Request, operation string, store SigningKeyStore, now time.Time) (*Principal, error) {
	denied := func() (*Principal, error) {
		return nil, errors.Unauthorized("INVALID_SIGNATURE", "invalid request credentials")
	}
	vals := make([]string, 3)
	for i, h := range []string{"X-Access-Key", "X-Timestamp", "X-Signature"} {
		v := r.Header.Values(h)
		if len(v) != 1 || strings.Contains(v[0], ",") {
			return denied()
		}
		vals[i] = v[0]
	}
	ak, ts, sig := vals[0], vals[1], vals[2]
	if !accessKeyPattern.MatchString(ak) || !timestampPattern.MatchString(ts) || !signaturePattern.MatchString(sig) {
		return denied()
	}
	seconds, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || seconds < now.Unix()-300 || seconds > now.Unix()+300 {
		return denied()
	}
	// Unknown/user-only operations never reach their handler under a key identity.
	if !allowsAPIKey(operation) {
		return nil, errors.Forbidden("USER_REQUIRED", "this operation requires a user identity")
	}
	if err := ValidateVPCReadRequest(r); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.ServiceUnavailable("SIGNING_STORE_UNAVAILABLE", "credential store unavailable")
	}
	key, err := store.LookupSigningKey(ctx, ak)
	if stderrors.Is(err, ErrSigningKeyRejected) {
		return denied()
	}
	if err != nil {
		return nil, errors.ServiceUnavailable("SIGNING_STORE_UNAVAILABLE", "credential store unavailable")
	}
	if key == nil || key.ID == 0 || key.TenantID == 0 {
		return denied()
	}
	if key.Secret == "" {
		return nil, errors.ServiceUnavailable("SIGNING_STORE_UNAVAILABLE", "credential secret unavailable")
	}
	expected := VPCSignature(key.Secret, r.URL.Path, ak, ts)
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return denied()
	}
	p := &Principal{Type: SubjectAPIKey, ID: key.ID, TenantID: key.TenantID, Roles: []string{key.Role}}
	if err := store.MarkSigningKeyUsed(ctx, key.ID); err != nil {
		return p, errors.ServiceUnavailable("SIGNING_STORE_UNAVAILABLE", "credential usage update unavailable")
	}
	if !key.RoleAllowed || key.Role == "" {
		p.Roles = nil
		return p, errors.Forbidden("SIGNING_ROLE_UNAVAILABLE", "bound role is not available")
	}
	return p, nil
}
