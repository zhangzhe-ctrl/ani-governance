package engine

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Contract covered by the approved T05 production-source fix (migration/patches/T05/
// P1-engine-authfrommd-status-error.patch): AuthFromMD must build its two dynamic
// error messages with status.Error, so the message is the plain concatenation of the
// literal and the caller's scheme for every scheme, including schemes that contain
// percent signs. Error codes, the token value returned, the "Bad authorization
// string" branch and the case-insensitive scheme comparison are unchanged upstream
// behaviour and are asserted here as well.

func incomingWithAuthorization(t *testing.T, value string) context.Context {
	t.Helper()
	md := metadata.MD{}
	if value != "" {
		md.Set(HeaderAuthorize, value)
	}
	return metadata.NewIncomingContext(context.Background(), md)
}

func TestAuthFromMDMissingAuthorizationHeader(t *testing.T) {
	token, err := AuthFromMD(incomingWithAuthorization(t, ""), BearerWord, ContextTypeGrpc)
	if token != "" {
		t.Fatalf("token = %q, want empty", token)
	}
	if err == nil {
		t.Fatal("err = nil, want an unauthenticated error")
	}
	if got := status.Code(err); got != codes.Unauthenticated {
		t.Errorf("code = %v, want %v", got, codes.Unauthenticated)
	}
	if got, want := status.Convert(err).Message(), "Request unauthenticated with "+BearerWord; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

func TestAuthFromMDValidBearerToken(t *testing.T) {
	ctx := incomingWithAuthorization(t, "Bearer eyJhbGciOi.Hello.World")
	token, err := AuthFromMD(ctx, BearerWord, ContextTypeGrpc)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got, want := token, "eyJhbGciOi.Hello.World"; got != want {
		t.Errorf("token = %q, want %q", got, want)
	}
}

func TestAuthFromMDSchemeIsMatchedCaseInsensitively(t *testing.T) {
	for _, headerScheme := range []string{"Bearer", "BEARER", "bearer", "BeArEr"} {
		ctx := incomingWithAuthorization(t, headerScheme+" tok")
		token, err := AuthFromMD(ctx, BearerWord, ContextTypeGrpc)
		if err != nil || token != "tok" {
			t.Errorf("scheme %q: token = %q, err = %v; want token %q and no error",
				headerScheme, token, err, "tok")
		}
	}
}

func TestAuthFromMDSchemeMismatch(t *testing.T) {
	ctx := incomingWithAuthorization(t, "Token abc")
	token, err := AuthFromMD(ctx, BearerWord, ContextTypeGrpc)
	if token != "" {
		t.Errorf("token = %q, want empty", token)
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want %v", status.Code(err), codes.Unauthenticated)
	}
	if got, want := status.Convert(err).Message(), "Request unauthenticated with "+BearerWord; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

func TestAuthFromMDMalformedAuthorizationHeader(t *testing.T) {
	for _, header := range []string{"Bearer", "BearerWithoutSpace", "NoSpaceHere"} {
		token, err := AuthFromMD(incomingWithAuthorization(t, header), BearerWord, ContextTypeGrpc)
		if token != "" {
			t.Errorf("header %q: token = %q, want empty", header, token)
		}
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("header %q: code = %v, want %v", header, status.Code(err), codes.Unauthenticated)
		}
		if got, want := status.Convert(err).Message(), "Bad authorization string"; got != want {
			t.Errorf("header %q: message = %q, want %q", header, got, want)
		}
	}
}

// TestAuthFromMDSchemeContainingPercentIsPlainText is the behaviour the approved fix
// exists for: a scheme carrying a % must never be interpreted as a format verb.
func TestAuthFromMDSchemeContainingPercentIsPlainText(t *testing.T) {
	const scheme = "Bearer%sz%d%!x(MISSING)"

	missing, err := AuthFromMD(incomingWithAuthorization(t, ""), scheme, ContextTypeGrpc)
	if missing != "" || err == nil {
		t.Fatalf("missing-header: token = %q, err = %v", missing, err)
	}
	if got, want := status.Convert(err).Message(), "Request unauthenticated with "+scheme; got != want {
		t.Errorf("missing-header message = %q, want the literal %q", got, want)
	}
	if strings.Contains(status.Convert(err).Message(), "MISSING") && !strings.Contains(scheme, "MISSING") {
		t.Errorf("message looks format-expanded: %q", status.Convert(err).Message())
	}

	mismatch, err := AuthFromMD(incomingWithAuthorization(t, "Other tok"), scheme, ContextTypeGrpc)
	if mismatch != "" || err == nil {
		t.Fatalf("mismatch: token = %q, err = %v", mismatch, err)
	}
	if got, want := status.Convert(err).Message(), "Request unauthenticated with "+scheme; got != want {
		t.Errorf("mismatch message = %q, want the literal %q", got, want)
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("code = %v, want %v", status.Code(err), codes.Unauthenticated)
	}

	// The percent-containing scheme is still an ordinary scheme when it matches.
	token, err := AuthFromMD(incomingWithAuthorization(t, scheme+" tok"), scheme, ContextTypeGrpc)
	if err != nil || token != "tok" {
		t.Errorf("matching percent scheme: token = %q, err = %v, want %q and no error", token, err, "tok")
	}
}

func TestAuthFromMDKratosContextTypeIsUnchanged(t *testing.T) {
	// ContextTypeKratosMetaData needs a transport header; without one the token is
	// empty and the unauthenticated branch is the same contract as above.
	token, err := AuthFromMD(context.Background(), BearerWord, ContextTypeKratosMetaData)
	if token != "" || err == nil {
		t.Fatalf("token = %q, err = %v, want empty token and an error", token, err)
	}
	if got, want := status.Convert(err).Message(), "Request unauthenticated with "+BearerWord; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}
