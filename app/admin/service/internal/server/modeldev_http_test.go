package server

import (
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/stretchr/testify/require"
	"go-wind-admin/app/admin/service/internal/service"
)

const modelDevCreateIntentJSON = `"name":"train","kind":"GENERAL_TRAINING","preset_id":"11111111-2222-4333-8444-555555555555","dataset_version_id":"22222222-3333-4444-8555-666666666666"`

func TestDecodeModelDevCreatePreservesParameterPresence(t *testing.T) {
	for _, test := range []struct {
		name    string
		extra   string
		present bool
	}{
		{name: "omitted"},
		{name: "explicit empty", extra: `,"general_parameters":[]`, present: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			in, err := decodeModelDevCreate(strings.NewReader(`{` + modelDevCreateIntentJSON + test.extra + `,"idempotency_key":"Case-\uD83D\uDE80"}`))
			require.NoError(t, err)
			require.Equal(t, "Case-🚀", in.IdempotencyKey)
			require.Equal(t, "train", in.Intent.Name)
			if test.present {
				require.NotNil(t, in.Intent.GeneralParameters)
				require.Empty(t, *in.Intent.GeneralParameters)
			} else {
				require.Nil(t, in.Intent.GeneralParameters)
			}
		})
	}
	// An escaped backslash is ordinary key content, not a surrogate escape.
	in, err := decodeModelDevCreate(strings.NewReader(`{` + modelDevCreateIntentJSON + `,"idempotency_key":"literal-\\uD800"}`))
	require.NoError(t, err)
	require.Equal(t, `literal-\uD800`, in.IdempotencyKey)
}

func TestDecodeModelDevCreateRejectsAmbiguousOrInjectedJSON(t *testing.T) {
	valid := `{` + modelDevCreateIntentJSON + `,"idempotency_key":"original"}`
	for _, test := range []struct{ name, raw string }{
		{"missing key", `{` + modelDevCreateIntentJSON + `}`},
		{"null key", `{` + modelDevCreateIntentJSON + `,"idempotency_key":null}`},
		{"empty key", `{` + modelDevCreateIntentJSON + `,"idempotency_key":""}`},
		{"control key", `{` + modelDevCreateIntentJSON + `,"idempotency_key":"secret\u0000value"}`},
		{"lone surrogate key", `{` + modelDevCreateIntentJSON + `,"idempotency_key":"secret\uD800"}`},
		{"duplicate key", strings.TrimSuffix(valid, "}") + `,"idempotency_key":"other"}`},
		{"escaped duplicate intent field", strings.TrimSuffix(valid, "}") + `,"na\u006de":"other"}`},
		{"nested duplicate", strings.TrimSuffix(valid, "}") + `,"general_parameters":[{"name":"epochs","type":"INTEGER","value":"3","value":"3"}]}`},
		{"null parameters", strings.TrimSuffix(valid, "}") + `,"general_parameters":null}`},
		{"injected tenant", strings.TrimSuffix(valid, "}") + `,"tenant_id":"secret-tenant"}`},
		{"injected actor", strings.TrimSuffix(valid, "}") + `,"actor":"secret-actor"}`},
		{"trailing object", valid + `{}`},
		{"invalid utf8", strings.Replace(valid, "original", string([]byte{0xff}), 1)},
		{"oversize", valid + strings.Repeat(" ", 16385)},
	} {
		t.Run(test.name, func(t *testing.T) {
			in, err := decodeModelDevCreate(strings.NewReader(test.raw))
			require.Equal(t, service.ModelDevCreateInput{}, in)
			require.Error(t, err)
			failure := errors.FromError(err)
			require.Equal(t, int32(400), failure.Code)
			require.Equal(t, "INVALID_MODELDEV_CREATE", failure.Reason)
			require.Equal(t, "invalid modeldev create request", failure.Message)
		})
	}
}
