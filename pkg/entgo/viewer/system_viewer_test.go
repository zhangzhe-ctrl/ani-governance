// 本文件为 system_viewer.go 的单元测试。
//
// SystemViewer 是系统后台任务（定时任务、服务间调用等非用户发起的请求）所使用的
// viewer 实现：身份字段全部为零值、不携带任何权限/角色/数据范围、永远处于平台视图
// 且被标记为系统上下文、权限判定一律放行、不产生审计日志。
// 这里逐项锁定上述语义，防止有人日后把系统身份改成携带租户或放行范围变化，
// 导致系统级任务被租户闸门误拦或数据范围被意外放大。
package viewer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tx7do/go-crud/viewer"
)

// TestNewSystemViewer_ReturnsSystemViewer 验证 NewSystemViewer 返回的接口值
// 动态类型就是 SystemViewer（空结构体，没有任何可变状态可供外部篡改）。
func TestNewSystemViewer_ReturnsSystemViewer(t *testing.T) {
	t.Parallel()

	vc := NewSystemViewer()
	require.NotNil(t, vc)
	require.IsType(t, SystemViewer{}, vc, "NewSystemViewer 必须返回 SystemViewer 动态类型")
}

// TestNewSystemViewerContext_RoundTrip 验证 NewSystemViewerContext 注入的系统身份
// 能被 viewer.FromContext 原样取回（注入链路可用），且注入前裸 context 中不存在 viewer。
func TestNewSystemViewerContext_RoundTrip(t *testing.T) {
	t.Parallel()

	base := context.Background()
	_, okBefore := viewer.FromContext(base)
	require.False(t, okBefore, "裸 context 中不应存在 viewer")

	ctx := NewSystemViewerContext(base)
	vc, ok := viewer.FromContext(ctx)
	require.True(t, ok, "NewSystemViewerContext 注入后必须能经 FromContext 取回")
	require.IsType(t, SystemViewer{}, vc, "取回的必须是 SystemViewer")

	// 取回的身份必须呈现系统语义，而非普通用户语义
	require.True(t, vc.IsSystemContext(), "系统身份必须报告 IsSystemContext=true")
	require.True(t, vc.IsPlatformContext(), "系统身份 tenant_id==0，属于平台视图")
	require.False(t, vc.IsTenantContext(), "系统身份不属于租户视图")
	require.Equal(t, uint64(0), vc.UserID())
	require.Equal(t, uint64(0), vc.TenantID())
}

// TestSystemViewer_FixedGetterSemantics 逐项锁定 SystemViewer 全部 getter 的固定返回值：
// 零值身份、空权限/角色/数据范围、空 TraceID、不放审计、平台视图、系统上下文。
// 这些值都是硬编码常量，任何一项漂移都会在这里被抓住。
func TestSystemViewer_FixedGetterSemantics(t *testing.T) {
	t.Parallel()

	vc := NewSystemViewer()
	require.NotNil(t, vc)

	identityGetters := []struct {
		name string
		got  func() any
		want any
	}{
		{"UserID 恒为 0", func() any { return vc.UserID() }, uint64(0)},
		{"TenantID 恒为 0", func() any { return vc.TenantID() }, uint64(0)},
		{"OrgUnitID 恒为 0", func() any { return vc.OrgUnitID() }, uint64(0)},
		{"Permissions 恒为空列表", func() any { return vc.Permissions() }, []string{}},
		{"Roles 恒为空列表", func() any { return vc.Roles() }, []string{}},
		{"DataScope 恒为空列表", func() any { return vc.DataScope() }, []viewer.DataScope{}},
		{"TraceID 恒为空串", func() any { return vc.TraceID() }, ""},
		{"ShouldAudit 恒为 false", func() any { return vc.ShouldAudit() }, false},
		{"IsPlatformContext 恒为 true", func() any { return vc.IsPlatformContext() }, true},
		{"IsTenantContext 恒为 false", func() any { return vc.IsTenantContext() }, false},
		{"IsSystemContext 恒为 true", func() any { return vc.IsSystemContext() }, true},
	}
	for _, tc := range identityGetters {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.got())
		})
	}
}

// TestSystemViewer_HasPermissionAlwaysGrants 验证系统身份的权限判定对任意
// 动作/资源组合一律放行（系统任务必须能执行全量维护操作）。
func TestSystemViewer_HasPermissionAlwaysGrants(t *testing.T) {
	t.Parallel()

	vc := NewSystemViewer()
	require.NotNil(t, vc)

	for _, comb := range [][2]string{
		{"read", "user"},
		{"update", "role"},
		{"", ""},
		{"任意动作", "任意资源"},
	} {
		require.True(t, vc.HasPermission(comb[0], comb[1]),
			"系统身份必须放行 action=%q resource=%q 的权限判定", comb[0], comb[1])
	}
}
