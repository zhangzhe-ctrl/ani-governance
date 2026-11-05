package bootstrap

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go-wind-admin/pkg/middleware/auth"
)

func releaseSeed(t *testing.T) (*initialSeed, []API) {
	t.Helper()
	doc, err := os.ReadFile("../../app/admin/service/cmd/server/assets/openapi.yaml")
	require.NoError(t, err)
	catalog, err := Catalog(doc)
	require.NoError(t, err)
	seed, err := loadInitialSeed(catalog)
	require.NoError(t, err)
	return seed, catalog
}

func TestInitialSeedReleaseCoverage(t *testing.T) {
	seed, catalog := releaseSeed(t)
	require.Len(t, seed.Routes, 482)
	require.Len(t, seed.ExcludedRoutes, 6)
	selected := seed.catalogForInit(catalog)
	require.Len(t, selected, 482)
	for _, api := range selected {
		require.NotEqual(t, "VMImageService", api.Module, api.Path)
	}
	require.Len(t, seed.Permissions, 369)
	require.Len(t, seed.Menus, 49) // 21 Console pages, 26 BOSS pages and two containers.
	require.Len(t, seed.FullPlanModules, 19)
	routes := map[string]initialRoute{}
	independent := 0
	for _, r := range seed.Routes {
		routes[r.Method+" "+r.Path] = r
		if r.Authentication == "independent" {
			independent++
		}
	}
	require.Equal(t, 10, independent)
	// These source policies are newer than the generic permcode table. The seed
	// must preserve the exact capability consumed by the real platform handler.
	for _, op := range auth.PlatformNetworkOperations {
		r, ok := routes[op.Method+" "+op.Path]
		if ok {
			require.Contains(t, r.Permissions, op.Permission, r.Path)
		}
	}
	require.ElementsMatch(t, []string{"instance:preset:read", "modeldev:compute-preset:read", "vm:compute-preset:read", "inference:compute-preset:read", "sandbox:preset:read"}, routes["GET /api/v1/compute-presets"].Permissions)
	require.Equal(t, []string{"modeldev:input:list"}, routes["GET /api/v1/modeldev/input-versions"].Permissions, "retain the main handler's input grant")
	require.Equal(t, "MODELDEV", routes["GET /api/v1/modeldev/input-versions"].Module)
	require.Equal(t, "SANDBOX", routes["GET /api/v1/sandbox"].Module)
	require.Equal(t, []string{"network:vpc:get"}, routes["GET /api/v1/networks/vpcs/{vpc_id}/associated-instances"].Permissions)
	require.Equal(t, []string{"network:subnet:get"}, routes["GET /api/v1/networks/subnets/{subnet_id}/associated-instances"].Permissions)
	require.Equal(t, []string{"modeldev:manage_execution"}, routes["POST /api/v1/modeldev/executions/{execution_id}:resume-model-registration"].Permissions)
	for _, p := range seed.Permissions {
		require.Regexp(t, `[一-龥]`, p.Name, p.Code)
	}
}

func TestInitialSeedRejectsIncompleteOrExpandedRelease(t *testing.T) {
	_, catalog := releaseSeed(t)
	_, err := loadInitialSeed(catalog[:len(catalog)-1])
	require.ErrorContains(t, err, "API catalog is incomplete")
	expanded := append(append([]API{}, catalog...), API{Method: "GET", Path: "/api/v1/extra", Operation: "Extra_Get", BusinessModule: "SYSTEM"})
	_, err = loadInitialSeed(expanded)
	require.ErrorContains(t, err, "expected exactly 488")
	changed := append([]API{}, catalog...)
	changed[0].BusinessModule = "SYSTEM"
	if changed[0].BusinessModule == catalog[0].BusinessModule {
		changed[0].Operation = "Other_Get"
	}
	_, err = loadInitialSeed(changed)
	require.ErrorContains(t, err, "differs from release")
}

func TestInitialSeedRejectsAuthorizationGaps(t *testing.T) {
	_, catalog := releaseSeed(t)
	for _, scenario := range []struct {
		name   string
		mutate func(*initialSeed)
	}{
		{"excluded route cannot grant", func(s *initialSeed) { s.ExcludedRoutes[0].Permissions = []string{"vm:read"} }},
		{"business route cannot be excluded", func(s *initialSeed) { s.ExcludedRoutes[0] = s.Routes[0] }},
		{"business exempt is not public", func(s *initialSeed) {
			for i := range s.Routes {
				r := &s.Routes[i]
				if r.Path == "/api/v1/modeldev/executions" && r.Method == "POST" {
					r.Permissions = nil
					r.Authentication = "independent"
				}
			}
		}},
		{"unknown grant", func(s *initialSeed) { s.Routes[0].Permissions = []string{"test:missing"} }},
		{"platform grant on tenant template", func(s *initialSeed) {
			for i := range s.Permissions {
				if s.Permissions[i].Code == "network:platform:public-pool:create" {
					s.Permissions[i].Roles = []string{"tenant"}
				}
			}
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var seed initialSeed
			require.NoError(t, json.Unmarshal(initialSeedJSON, &seed))
			scenario.mutate(&seed)
			require.Error(t, seed.validate(catalog))
		})
	}
}
