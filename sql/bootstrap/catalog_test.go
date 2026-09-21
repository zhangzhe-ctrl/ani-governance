package bootstrap

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCatalogValidation(t *testing.T) {
	_, err := Catalog([]byte("openapi: 3.0.0\ninfo: {title: test, version: '1'}\n"))
	require.Error(t, err)
	_, err = Catalog([]byte("openapi: 3.0.0\ninfo: {title: test, version: '1'}\npaths:\n  /unknown:\n    get:\n      operationId: Unknown_Get\n      tags: [UnknownService]\n"))
	require.ErrorContains(t, err, "business module")
}
