package ent_test

import (
	"context"
	"testing"

	"github.com/tx7do/go-crud/viewer"

	"go-wind-admin/app/admin/service/internal/data/ent/dicttype"
)

// TestTenantGuardDeleteOneCreate 补充验证升级后的两个具体场景：
//  1. Create 防伪造：租户用户显式 SetTenantID(他租户) 必须被强制覆盖回自身租户；
//  2. DeleteOne：跨租户按主键删除必须 0 行命中（行完好），本租户删除正常。
func TestTenantGuardDeleteOneCreate(t *testing.T) {
	client := openGuardTestClient(t)
	ctx := context.Background()

	// --- Create 防伪造 ---
	ctx1 := tenantCtx(ctx, 1)
	client.DictType.Create().SetTypeCode("FAKE_T2").SetTypeName("fake").SetIsEnabled(true).
		SetTenantID(2). // 试图把记录伪装成租户 2 的
		ExecX(ctx1)
	row := client.DictType.Query().Where(dicttype.TypeCodeEQ("FAKE_T2")).OnlyX(ctx1)
	if row.TenantID == nil || *row.TenantID != 1 {
		t.Fatalf("create tenant_id overwrite failed: got tid=%d, want 1", row.TenantID)
	}

	// --- DeleteOne 跨租户 ---
	ctx2 := tenantCtx(ctx, 2)
	client.DictType.Create().SetTypeCode("DEL_T2").SetTypeName("victim").SetIsEnabled(true).ExecX(ctx2)
	t2ID := dictTypeIDByCode(client, ctx2, "DEL_T2")

	err := client.DictType.DeleteOneID(t2ID).Exec(ctx1)
	if err == nil {
		// ent DeleteOne 命中 0 行返回 NotFound 错误；未报错说明删到了他租户行
		t.Fatalf("cross-tenant DeleteOne unexpectedly succeeded")
	}
	if client.DictType.Query().Where(dicttype.TypeCodeEQ("DEL_T2")).ExistX(ctx2) != true {
		t.Fatal("cross-tenant DeleteOne removed the row")
	}

	// --- DeleteOne 本租户 ---
	ctx1b := tenantCtx(ctx, 1)
	client.DictType.Create().SetTypeCode("DEL_T1").SetTypeName("own").SetIsEnabled(true).ExecX(ctx1b)
	t1ID := dictTypeIDByCode(client, ctx1b, "DEL_T1")
	if err := client.DictType.DeleteOneID(t1ID).Exec(ctx1b); err != nil {
		t.Fatalf("own-tenant DeleteOne failed: %v", err)
	}
	if client.DictType.Query().Where(dicttype.TypeCodeEQ("DEL_T1")).ExistX(ctx1b) {
		t.Fatal("own-tenant DeleteOne did not remove the row")
	}

	// --- 平台上下文跨租户删除：放行（全权） ---
	platformViewer := mockViewerPlatform{}
	pctx := viewer.WithContext(ctx, platformViewer)
	if err := client.DictType.DeleteOneID(t2ID).Exec(pctx); err != nil {
		t.Fatalf("platform cross-tenant DeleteOne should be allowed: %v", err)
	}
}

// mockViewerPlatform 平台管理员视角。
type mockViewerPlatform struct {
	mockViewer
}

func (mockViewerPlatform) IsPlatformContext() bool { return true }
