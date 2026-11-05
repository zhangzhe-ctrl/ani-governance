package server

import (
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
)

const sandboxUploadFileLimit = 1 << 20
const sandboxUploadEnvelopeLimit = sandboxUploadFileLimit + 64<<10

func isSandboxMultipart(r *http.Request) bool {
	// Include malformed multipart content types so they are rejected by the
	// multipart decoder rather than interpreted as JSON.
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))), "multipart/form-data")
}

// decodeSandboxMultipart never uses ParseMultipartForm, which can write user
// content to disk. Both the file and the entire envelope have hard limits even
// when Content-Length is missing or dishonest.
func decodeSandboxMultipart(r *http.Request, request *view.WriteSandboxFileRequest) error {
	invalid := func() error { return kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid sandbox upload") }
	tooLarge := func() error { return kratoserrors.New(413, "SANDBOX_FILE_TOO_LARGE", "file too large") }
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" || r.Body == nil || r.URL.ForceQuery || r.URL.RawQuery != "" {
		return invalid()
	}
	if r.ContentLength > sandboxUploadEnvelopeLimit {
		return tooLarge()
	}
	limited := &io.LimitedReader{R: r.Body, N: sandboxUploadEnvelopeLimit + 1}
	reader := multipart.NewReader(limited, params["boundary"])
	data := &view.MutateSandboxFileData{}
	seen := make(map[string]bool, 5)
	for {
		part, err := reader.NextRawPart()
		if limited.N == 0 {
			return tooLarge()
		}
		if err == io.EOF {
			// Count any MIME epilogue too, including chunked requests without a
			// Content-Length. The multipart reader may have buffered part of it.
			if _, err := io.Copy(io.Discard, limited); err != nil {
				return invalid()
			}
			if limited.N == 0 {
				return tooLarge()
			}
			break
		}
		if err != nil || len(part.Header.Values("Content-Disposition")) != 1 || len(part.Header.Values("Content-Transfer-Encoding")) != 0 {
			return invalid()
		}
		name := part.FormName()
		if seen[name] || (name != "file" && name != "path" && name != "idempotency_key" && name != "expected_version" && name != "overwrite") {
			return invalid()
		}
		seen[name] = true
		max := int64(4096)
		if name == "file" {
			max = sandboxUploadFileLimit
			if part.FileName() == "" {
				return invalid()
			}
		} else if part.FileName() != "" {
			return invalid()
		}
		contents, err := io.ReadAll(io.LimitReader(part, max+1))
		if limited.N == 0 || int64(len(contents)) > max {
			if name == "file" || limited.N == 0 {
				return tooLarge()
			}
			return invalid()
		}
		if err != nil {
			return invalid()
		}
		if name == "file" {
			data.ContentBase64 = base64.StdEncoding.EncodeToString(contents)
			continue
		}
		if !utf8.Valid(contents) {
			return invalid()
		}
		value := string(contents)
		switch name {
		case "path":
			data.Path = value
		case "idempotency_key":
			data.IdempotencyKey = value
		case "expected_version":
			version, err := strconv.ParseInt(value, 10, 64)
			if err != nil || version < 0 || strconv.FormatInt(version, 10) != value {
				return invalid()
			}
			data.ExpectedVersion = version
		case "overwrite":
			if value != "true" && value != "false" {
				return invalid()
			}
			data.Overwrite = value == "true"
		}
	}
	if !seen["file"] || !seen["path"] || !seen["idempotency_key"] {
		return invalid()
	}
	request.Data = data
	return nil
}
