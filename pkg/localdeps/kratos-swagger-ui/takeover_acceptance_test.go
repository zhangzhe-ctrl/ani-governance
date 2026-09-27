// 接管验收测试（T12）：证明 pkg/localdeps 里的这份 swagger UI 仍然可用，
// 而不是只把包路径换掉了事。
//
// 覆盖目标：
//   - 启用时（NewRestServer 的 EnableSwagger 分支所做的事）：注册后 HTML 首页可取、
//     标题生效、内存 OpenAPI 按 basePath/openapi.<ext> 可取、静态资源由打包的
//     statigz/swaggest FS 提供。
//   - 本地文件模式：命中 pattern 时返回文件内容；读不到文件时保持既有的
//     “打印错误 + 不注册该路由” 行为，不静默改成 panic 或 200。
//   - 禁用时：未注册即无 /docs/ 路由，维持注册前的旧行为。
//   - RegisterSwaggerUIServer（WithTitle 风格的简易入口）仍然工作。
package swaggerUI

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/swaggest/swgui/v5/static"

	"go-wind-admin/pkg/localdeps/kratos-swagger-ui/internal/swagger"
)

const (
	probeOpenApi = "openapi: 3.0.3\ninfo:\n  title: takeover-probe\n  version: t12\npaths: {}\n"
	probeTitle   = "ANI Governance T12"
	probeBase    = "/docs/"
)

// servedOn registers the swagger UI on a real Kratos HTTP mux and serves it over
// httptest, so the prefixes are resolved by the same router the application uses.
func servedOn(t *testing.T, register func(srv *khttp.Server)) *httptest.Server {
	t.Helper()
	mux := khttp.NewServer()
	register(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// firstStaticAsset names a file that the packaged UI assets really contain, so the
// asset assertion does not depend on a guessed path.
func firstStaticAsset(t *testing.T) string {
	t.Helper()
	var names []string
	require.NoError(t, fs.WalkDir(static.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		names = append(names, path)
		return nil
	}))
	sort.Strings(names)
	require.NotEmpty(t, names, "the packaged swagger UI assets are empty")
	return names[0]
}

func get(t *testing.T, url string) (int, http.Header, string) {
	t.Helper()
	resp, err := http.Get(url) //nolint:gosec // loopback httptest server
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header, string(body)
}

// TestTakenOverUiServesHtmlAssetsAndMemoryOpenApi is the enabled path: exactly the
// call NewRestServer makes behind EnableSwagger.
func TestTakenOverUiServesHtmlAssetsAndMemoryOpenApi(t *testing.T) {
	ts := servedOn(t, func(srv *khttp.Server) {
		RegisterSwaggerUIServerWithOption(srv,
			WithTitle(probeTitle),
			WithMemoryData([]byte(probeOpenApi), "yaml"),
			WithBasePath(probeBase),
		)
	})

	status, header, html := get(t, ts.URL+probeBase)
	require.Equal(t, http.StatusOK, status, "the UI index must be served")
	assert.Contains(t, header.Get("Content-Type"), "text/html")
	assert.Contains(t, html, probeTitle, "the configured title must reach the page")
	assert.NotContains(t, html, "{{ .BasePath }}", "the asset placeholders must be substituted")
	assert.Contains(t, html, strings.TrimSuffix(probeBase, "/")+"/openapi.yaml",
		"the page must point at the in-memory document route")

	status, header, doc := get(t, ts.URL+strings.TrimSuffix(probeBase, "/")+"/openapi.yaml")
	require.Equal(t, http.StatusOK, status, "the in-memory OpenAPI document must be fetchable")
	assert.Equal(t, probeOpenApi, doc, "the document must come back byte-for-byte")
	// openJsonFileHandler only writes the bytes and never sets a Content-Type, so Go
	// sniffs the body. That is the behaviour being preserved, asserted rather than
	// replaced with the content type one might wish for.
	assert.Equal(t, "text/plain; charset=utf-8", header.Get("Content-Type"))

	asset := firstStaticAsset(t)
	status, _, body := get(t, ts.URL+probeBase+asset)
	require.Equal(t, http.StatusOK, status, "the packaged asset %q must be served", asset)
	assert.NotEmpty(t, body)
}

// TestOptionsDoNotLeakBetweenRegistrations keeps the option plumbing honest: every
// registration builds its own configuration, so a second server cannot inherit the
// first one's document, and the default base path the business call relies on stays
// "/docs/".
func TestOptionsDoNotLeakBetweenRegistrations(t *testing.T) {
	cfg := swagger.NewConfig()
	assert.Equal(t, "/docs/", cfg.BasePath, "the default base path is what NewRestServer relies on")

	first := servedOn(t, func(srv *khttp.Server) {
		RegisterSwaggerUIServerWithOption(srv,
			WithTitle("first-ui"),
			WithMemoryData([]byte(probeOpenApi), "yaml"),
			WithBasePath("/one/"),
		)
	})
	second := servedOn(t, func(srv *khttp.Server) {
		RegisterSwaggerUIServerWithOption(srv,
			WithTitle("second-ui"),
			WithBasePath("/two/"),
		)
	})

	_, _, one := get(t, first.URL+"/one/")
	assert.Contains(t, one, "first-ui")
	assert.NotContains(t, one, "second-ui")

	_, _, two := get(t, second.URL+"/two/")
	assert.Contains(t, two, "second-ui")
	assert.NotContains(t, two, "first-ui")
	assert.NotContains(t, two, "/one/openapi.yaml", "the second UI must not point at the first one's document")
}

// TestLocalFileServesDocumentAndMissingFileKeepsOldBehaviour covers both local-file
// branches, including the error branch, which must stay "report and do not register".
func TestLocalFileServesDocumentAndMissingFileKeepsOldBehaviour(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "openapi.yaml")
	require.NoError(t, os.WriteFile(doc, []byte(probeOpenApi), 0o600))

	ts := servedOn(t, func(srv *khttp.Server) {
		RegisterSwaggerUIServerWithOption(srv,
			WithTitle(probeTitle),
			WithLocalFile(doc),
			WithBasePath(probeBase),
		)
	})

	status, _, body := get(t, ts.URL+strings.TrimSuffix(probeBase, "/")+"/openapi.yaml")
	require.Equal(t, http.StatusOK, status, "the local document must be served")
	assert.Equal(t, probeOpenApi, body)

	// A file that cannot be read must not register the document route and must not
	// take the process down; the UI page itself keeps working.
	missing := filepath.Join(dir, "does-not-exist.yaml")
	broken := servedOn(t, func(srv *khttp.Server) {
		RegisterSwaggerUIServerWithOption(srv,
			WithTitle(probeTitle),
			WithLocalFile(missing),
			WithBasePath(probeBase),
		)
	})
	status, _, _ = get(t, broken.URL+strings.TrimSuffix(probeBase, "/")+"/openapi.yaml")
	assert.NotEqual(t, http.StatusOK, status, "an unreadable document must not answer 200")
	status, _, html := get(t, broken.URL+probeBase)
	assert.Equal(t, http.StatusOK, status, "the UI page must survive a failed document load")
	assert.Contains(t, html, probeTitle)
}

// TestDisabledRegistrationServesNothing is the disabled path: when the config gate is
// off the application registers nothing, so the UI prefixes keep answering the way an
// unregistered route does.
func TestDisabledRegistrationServesNothing(t *testing.T) {
	ts := servedOn(t, func(_ *khttp.Server) {})

	for _, path := range []string{probeBase, strings.TrimSuffix(probeBase, "/") + "/openapi.yaml", probeBase + "swagger-ui.css"} {
		status, _, _ := get(t, ts.URL+path)
		assert.Equal(t, http.StatusNotFound, status, "nothing may answer %q when the UI is not registered", path)
	}
}

// TestRegisterSwaggerUIServerSimpleEntry keeps the other business entry point honest.
func TestRegisterSwaggerUIServerSimpleEntry(t *testing.T) {
	ts := servedOn(t, func(srv *khttp.Server) {
		RegisterSwaggerUIServer(srv, probeTitle, "/custom/openapi.yaml", probeBase)
	})

	status, _, html := get(t, ts.URL+probeBase)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, html, probeTitle)
	assert.Contains(t, html, "/custom/openapi.yaml", "the remote document URL must be used verbatim")
}
