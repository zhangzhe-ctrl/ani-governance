package viewer

import (
	"testing"

	"github.com/tx7do/go-crud/viewer"

	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
)

func one(t *testing.T, typ viewer.ScopeType, targets []uint64) []viewer.DataScope {
	t.Helper()
	return []viewer.DataScope{{ScopeType: typ, TargetIDs: targets}}
}

// TestBuildDataScopes 验证令牌聚合数据范围到库层执行结构的映射矩阵：
// ALL/SELF/UNIT_* 映射、UNSPECIFIED 剔除、旧单值回退、混合并集。
func TestBuildDataScopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		scopes  []identityV1.DataScope
		units   []uint64
		legacy  identityV1.DataScope
		wantLen int
		want    []viewer.DataScope
	}{
		{
			name:    "all",
			scopes:  []identityV1.DataScope{identityV1.DataScope_ALL},
			wantLen: 1,
			want:    one(t, viewer.ScopeTypeAll, nil),
		},
		{
			name:    "self",
			scopes:  []identityV1.DataScope{identityV1.DataScope_SELF},
			wantLen: 1,
			want:    one(t, viewer.ScopeTypeSelf, nil),
		},
		{
			name:    "unit_only carries targets",
			scopes:  []identityV1.DataScope{identityV1.DataScope_UNIT_ONLY},
			units:   []uint64{5},
			wantLen: 1,
			want:    one(t, viewer.ScopeTypeUnit, []uint64{5}),
		},
		{
			name:    "selected_units carries targets",
			scopes:  []identityV1.DataScope{identityV1.DataScope_SELECTED_UNITS},
			units:   []uint64{1, 2},
			wantLen: 1,
			want:    one(t, viewer.ScopeTypeUnit, []uint64{1, 2}),
		},
		{
			name:    "unspecified dropped leaves empty",
			scopes:  []identityV1.DataScope{identityV1.DataScope_DATA_SCOPE_UNSPECIFIED},
			units:   []uint64{9},
			wantLen: 0,
			want:    []viewer.DataScope{},
		},
		{
			name:    "legacy single value fallback",
			scopes:  nil,
			legacy:  identityV1.DataScope_ALL,
			wantLen: 1,
			want:    one(t, viewer.ScopeTypeAll, nil),
		},
		{
			name: "mixed union",
			scopes: []identityV1.DataScope{
				identityV1.DataScope_SELF,
				identityV1.DataScope_UNIT_AND_CHILD,
			},
			units:   []uint64{3},
			wantLen: 2,
			want: []viewer.DataScope{
				{ScopeType: viewer.ScopeTypeSelf},
				{ScopeType: viewer.ScopeTypeUnit, TargetIDs: []uint64{3}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildDataScopes(tc.scopes, tc.units, tc.legacy)
			if len(got) != tc.wantLen {
				t.Fatalf("len = %d, want %d", len(got), tc.wantLen)
			}
			for i := range tc.want {
				if got[i].ScopeType != tc.want[i].ScopeType {
					t.Fatalf("scope[%d].type = %v, want %v", i, got[i].ScopeType, tc.want[i].ScopeType)
				}
				if len(got[i].TargetIDs) != len(tc.want[i].TargetIDs) {
					t.Fatalf("scope[%d].targets len = %d, want %d", i, len(got[i].TargetIDs), len(tc.want[i].TargetIDs))
				}
			}
		})
	}
}
