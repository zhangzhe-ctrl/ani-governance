package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/go-kratos/kratos/v2/errors"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	view "go-wind-admin/api/gen/go/catalog/service/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type ImageOperationPolicy struct {
	Method, Path, Permission string
	ReadOnly                 bool
}

// Catalog mapping only; authentication, tenant subscription and current Casbin
// policy are still mandatory. Presence here never grants a role or API key.
var ImageOperations = map[string]ImageOperationPolicy{
	"/admin.service.v1.ImageService/EnsureImageSpace":           {"POST", "/api/v1/images/space:enable", "image:space:enable", false},
	"/admin.service.v1.ImageService/GetImageSpace":              {"GET", "/api/v1/images/space", "image:space:get", true},
	"/admin.service.v1.ImageService/GetPublisherCredential":     {"GET", "/api/v1/images/publisher-credential", "image:credential:get", true},
	"/admin.service.v1.ImageService/IssuePublisherCredential":   {"POST", "/api/v1/images/publisher-credential:issue", "image:credential:issue", false},
	"/admin.service.v1.ImageService/ResetPublisherCredential":   {"POST", "/api/v1/images/publisher-credential:reset", "image:credential:reset", false},
	"/admin.service.v1.ImageService/DisablePublisherCredential": {"POST", "/api/v1/images/publisher-credential:disable", "image:credential:disable", false},
	"/admin.service.v1.ImageService/RegisterImage":              {"POST", "/api/v1/images/registrations", "image:registration:create", false},
	"/admin.service.v1.ImageService/GetImage":                   {"GET", "/api/v1/images/registrations/{image_id}", "image:registration:get", true},
	"/admin.service.v1.ImageService/ListImages":                 {"GET", "/api/v1/images/registrations", "image:registration:list", true},
	"/admin.service.v1.ImageService/UpdateImage":                {"PATCH", "/api/v1/images/registrations/{image_id}", "image:registration:update", false},
	"/admin.service.v1.ImageService/UnregisterImage":            {"POST", "/api/v1/images/registrations/{image_id}:unregister", "image:registration:unregister", false},
}

func IsImageOperation(operation string) bool { _, ok := ImageOperations[operation]; return ok }
func ImagePermission(method, path string) (string, bool) {
	for _, p := range ImageOperations {
		if p.Method == method && p.Path == path {
			return p.Permission, true
		}
	}
	return "", false
}
func IsImagePath(path string) bool { return strings.HasPrefix(path, "/api/v1/images/") }

type imageRequestDigestKey struct{}
type imageRequestDigest struct{ Operation, Digest string }

func imageHTTPRoute(r *http.Request) (string, proto.Message, string) {
	path := r.URL.Path
	var op string
	var message proto.Message
	var id string
	switch {
	case r.Method == "POST" && path == "/api/v1/images/space:enable":
		op = "EnsureImageSpace"
		message = &view.EnsureImageSpaceRequest{}
	case r.Method == "GET" && path == "/api/v1/images/space":
		op = "GetImageSpace"
		message = &view.GetImageSpaceRequest{}
	case r.Method == "GET" && path == "/api/v1/images/publisher-credential":
		op = "GetPublisherCredential"
		message = &view.GetPublisherCredentialRequest{}
	case r.Method == "POST" && path == "/api/v1/images/publisher-credential:issue":
		op = "IssuePublisherCredential"
		message = &view.IssuePublisherCredentialRequest{}
	case r.Method == "POST" && path == "/api/v1/images/publisher-credential:reset":
		op = "ResetPublisherCredential"
		message = &view.ResetPublisherCredentialRequest{}
	case r.Method == "POST" && path == "/api/v1/images/publisher-credential:disable":
		op = "DisablePublisherCredential"
		message = &view.DisablePublisherCredentialRequest{}
	case r.Method == "POST" && path == "/api/v1/images/registrations":
		op = "RegisterImage"
		message = &view.RegisterImageRequest{}
	case r.Method == "GET" && path == "/api/v1/images/registrations":
		op = "ListImages"
		message = &view.ListImagesRequest{}
	default:
		prefix := "/api/v1/images/registrations/"
		if !strings.HasPrefix(path, prefix) {
			return "", nil, ""
		}
		id = strings.TrimPrefix(path, prefix)
		switch {
		case r.Method == "POST" && strings.HasSuffix(id, ":unregister"):
			id = strings.TrimSuffix(id, ":unregister")
			op = "UnregisterImage"
			message = &view.UnregisterImageRequest{}
		case r.Method == "GET":
			op = "GetImage"
			message = &view.GetImageRequest{}
		case r.Method == "PATCH":
			op = "UpdateImage"
			message = &view.UpdateImageRequest{}
		default:
			return "", nil, ""
		}
		if !regexp.MustCompile(`^img_[a-f0-9]{32}$`).MatchString(id) {
			return "", nil, ""
		}
	}
	return "/admin.service.v1.ImageService/" + op, message, id
}

// ImageHTTPFilter runs before generated decoding. It rejects unsupported body
// fields instead of allowing a permissive decoder to silently discard identity
// injection, and hashes the exact bounded bytes for AK request signing.
func ImageHTTPFilter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsImagePath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/images/publisher-credential") {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")
		}
		fail := func() {
			khttp.DefaultErrorEncoder(w, r, errors.BadRequest("INVALID_IMAGE_REQUEST", "invalid Image HTTP request"))
		}
		if r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}
		operation, message, id := imageHTTPRoute(r)
		if operation == "" || r.URL.RawPath != "" || r.URL.ForceQuery || strings.Contains(r.URL.EscapedPath(), "%") || len(r.URL.RawQuery) > 8192 {
			fail()
			return
		}
		for _, key := range []string{"X-Ani-Tenant-Id", "X-Tenant-Id", "X-Ani-Actor", "X-Ani-Operator", "X-Ani-Request-Id", "X-Resource-Tenant-Id"} {
			if len(r.Header.Values(key)) != 0 {
				fail()
				return
			}
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			fail()
			return
		}
		allowed := map[string]bool{}
		switch operation {
		case "/admin.service.v1.ImageService/GetImage":
			allowed["scope"] = true
		case "/admin.service.v1.ImageService/ListImages":
			for _, key := range []string{"scope", "search", "purposes", "accelerator", "limit", "cursor"} {
				allowed[key] = true
			}
		}
		for key, values := range query {
			if !allowed[key] || len(values) == 0 || (key != "purposes" && len(values) != 1) || (key == "purposes" && len(values) > 5) {
				fail()
				return
			}
		}
		var body []byte
		if r.Body != nil {
			body, err = io.ReadAll(io.LimitReader(r.Body, 16385))
			_ = r.Body.Close()
			if err != nil || len(body) > 16384 {
				fail()
				return
			}
		}
		if r.Method == "GET" {
			if len(body) != 0 {
				fail()
				return
			}
		} else {
			media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || media != "application/json" || len(body) == 0 {
				fail()
				return
			}
			if err = (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, message); err != nil {
				fail()
				return
			}
			if id != "" {
				m := message.ProtoReflect()
				field := m.Descriptor().Fields().ByName("image_id")
				if field != nil {
					value := m.Get(field).String()
					if value != "" && value != id {
						fail()
						return
					}
				}
			}
		}
		sum := sha256.Sum256(body)
		ctx := context.WithValue(r.Context(), imageRequestDigestKey{}, imageRequestDigest{operation, hex.EncodeToString(sum[:])})
		r = r.WithContext(ctx)
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	})
}

// Query must be url.Values.Encode() for signed Image calls; bodies are signed
// byte-for-byte. The same principal still passes current tenant/Casbin checks.
func ImageSignature(secret, method, path, query, accessKey, timestamp string, body []byte) string {
	sum := sha256.Sum256(body)
	return imageSignatureDigest(secret, method, path, query, accessKey, timestamp, hex.EncodeToString(sum[:]))
}
func imageSignatureDigest(secret, method, path, query, accessKey, timestamp, digest string) string {
	canonical := strings.Join([]string{"ANI-HMAC-SHA256", method, path, query, accessKey, timestamp, digest}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}
