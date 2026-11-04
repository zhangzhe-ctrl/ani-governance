package server

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var errInvalidJSONRequest = errors.New("invalid JSON request")

// decodeDeleteInput accepts an empty body and one idempotency key, supplied
// either as a header or using the query mapping generated from the Proto.
func decodeDeleteInput(request *http.Request, withVersion bool) (string, int64, error) {
	if request == nil || request.URL == nil || request.URL.ForceQuery || request.URL.RawPath != "" && request.URL.RawPath != request.URL.Path {
		return "", 0, errInvalidJSONRequest
	}
	for _, header := range []string{"X-Ani-Tenant-Id", "X-Tenant-Id", "X-Ani-Actor", "X-Ani-Operator", "X-Resource-Tenant-Id"} {
		if len(request.Header.Values(header)) != 0 {
			return "", 0, errInvalidJSONRequest
		}
	}
	keys := request.Header.Values("Idempotency-Key")
	if len(keys) > 1 {
		return "", 0, errInvalidJSONRequest
	}
	if request.Body != nil {
		raw, err := io.ReadAll(io.LimitReader(request.Body, 1))
		if err != nil || len(raw) != 0 {
			return "", 0, errInvalidJSONRequest
		}
	}
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		return "", 0, errInvalidJSONRequest
	}
	var version int64
	var queryKey string
	var hasQueryKey, hasVersion bool
	for name, values := range query {
		if len(values) != 1 {
			return "", 0, errInvalidJSONRequest
		}
		switch {
		case !withVersion && name == "idempotency_key", withVersion && name == "data.idempotency_key":
			queryKey, hasQueryKey = values[0], true
		case withVersion && (name == "expected_version" || name == "data.expected_version"):
			if hasVersion {
				return "", 0, errInvalidJSONRequest
			}
			version, err = strconv.ParseInt(values[0], 10, 64)
			if err != nil || version < 0 || strconv.FormatInt(version, 10) != values[0] {
				return "", 0, errInvalidJSONRequest
			}
			hasVersion = true
		default:
			return "", 0, errInvalidJSONRequest
		}
	}
	if len(keys) == 1 && hasQueryKey {
		return "", 0, errInvalidJSONRequest
	}
	key := queryKey
	if len(keys) == 1 {
		key = keys[0]
	}
	if key == "" || len([]rune(key)) > 256 || strings.TrimSpace(key) != key || strings.Contains(key, ",") {
		return "", 0, errInvalidJSONRequest
	}
	return key, version, nil
}

// Public command decoders reject unknown fields and caller-supplied internal
// identity. Each business boundary maps this error to its own public reason.
func decodeStrictJSONHTTP(request *http.Request, message proto.Message) error {
	return decodeStrictJSONHTTPWithLimit(request, message, 65536)
}

func decodeStrictJSONHTTPWithLimit(request *http.Request, message proto.Message, limit int64) error {
	if request == nil || request.Body == nil || request.URL == nil || request.URL.RawQuery != "" || request.URL.ForceQuery || request.URL.RawPath != "" && request.URL.RawPath != request.URL.Path {
		return errInvalidJSONRequest
	}
	media, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return errInvalidJSONRequest
	}
	for _, header := range []string{"X-Ani-Tenant-Id", "X-Tenant-Id", "X-Ani-Actor", "X-Ani-Operator", "X-Resource-Tenant-Id"} {
		if len(request.Header.Values(header)) != 0 {
			return errInvalidJSONRequest
		}
	}
	raw, err := io.ReadAll(io.LimitReader(request.Body, limit+1))
	if err != nil || len(raw) == 0 || int64(len(raw)) > limit {
		return errInvalidJSONRequest
	}
	if err = (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(raw, message); err != nil {
		return errInvalidJSONRequest
	}
	return nil
}
