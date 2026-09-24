//go:build quota_pg

package data

import (
	"context"
	"testing"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/trans"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"

	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	"go-wind-admin/app/admin/service/internal/data/ent/planquota"
	"go-wind-admin/app/admin/service/internal/data/enttest"
)

// TestPlanQuotaRepoPostgres_QuotaCodeCompat 覆盖 §5.2 请求兼容行为：
// 只传 code / 只传旧枚举 / 冲突拒绝 / gpu.count 读取旧枚举为 nil。
func TestPlanQuotaRepoPostgres_QuotaCodeCompat(t *testing.T) {
	entClient := enttest.NewQuotaPGClient(t)
	repo := newPlanQuotaRepoPostgres(t, entClient)
	ctx := enttest.NewSystemViewerCtx(context.Background())

	parent, err := entClient.Client().Plan.Create().
		SetNillableName(trans.Ptr("sqlite_pq_compat_plan")).
		Save(ctx)
	require.NoError(t, err)

	// 1) 只传 quota_code=gpu.count：OK；读取旧枚举为 nil
	require.NoError(t, repo.Create(ctx, &identityV1.CreatePlanQuotaRequest{
		Data: &identityV1.PlanQuota{
			PlanId:     &parent.ID,
			QuotaCode:  trans.Ptr(QuotaCodeGpuCount),
			QuotaValue: trans.Ptr(uint64(8)),
		},
	}), "只传 gpu.count 应成功")

	// 2) 只传旧枚举 STORAGE：映射为 storage.bytes
	require.NoError(t, repo.Create(ctx, &identityV1.CreatePlanQuotaRequest{
		Data: &identityV1.PlanQuota{
			PlanId:     &parent.ID,
			QuotaType:  identityV1.PlanQuota_STORAGE.Enum(),
			QuotaValue: trans.Ptr(uint64(1024)),
		},
	}), "只传旧枚举应成功并映射 storage.bytes")

	// 3) code/type 冲突：400 INVALID_QUOTA_REQUEST
	err = repo.Create(ctx, &identityV1.CreatePlanQuotaRequest{
		Data: &identityV1.PlanQuota{
			PlanId:     &parent.ID,
			QuotaCode:  trans.Ptr(QuotaCodeUserCount),
			QuotaType:  identityV1.PlanQuota_STORAGE.Enum(),
			QuotaValue: trans.Ptr(uint64(1)),
		},
	})
	require.Error(t, err)
	require.Equal(t, "INVALID_QUOTA_REQUEST", codeOf(t, err))

	// 4) 都缺失：400
	err = repo.Create(ctx, &identityV1.CreatePlanQuotaRequest{
		Data: &identityV1.PlanQuota{PlanId: &parent.ID, QuotaValue: trans.Ptr(uint64(1))},
	})
	require.Error(t, err)
	require.Equal(t, "INVALID_QUOTA_REQUEST", codeOf(t, err))

	// 5) 缺 plan_id / quota_value：400
	err = repo.Create(ctx, &identityV1.CreatePlanQuotaRequest{
		Data: &identityV1.PlanQuota{QuotaCode: trans.Ptr(QuotaCodeGpuCount), QuotaValue: trans.Ptr(uint64(1))},
	})
	require.Error(t, err)
	require.Equal(t, "INVALID_QUOTA_REQUEST", codeOf(t, err))

	// 读取投影断言
	listed, err := repo.List(ctx, &paginationV1.PagingRequest{NoPaging: trans.Ptr(true)})
	require.NoError(t, err)
	byCode := map[string]*identityV1.PlanQuota{}
	for _, item := range listed.Items {
		byCode[item.GetQuotaCode()] = item
	}
	require.Nil(t, byCode[QuotaCodeGpuCount].QuotaType, "gpu.count 旧枚举投影必须为 nil/UNSPECIFIED")
	require.Equal(t, identityV1.PlanQuota_STORAGE, byCode[QuotaCodeStorage].GetQuotaType(), "旧三项保持一致旧枚举")

	// 6) Update 掩码改 planId：400
	rows, err := entClient.Client().PlanQuota.Query().All(ctx)
	require.NoError(t, err)
	var gpuRowID uint32
	for _, r := range rows {
		if r.QuotaCode == QuotaCodeGpuCount {
			gpuRowID = r.ID
		}
	}
	err = repo.Update(ctx, &identityV1.UpdatePlanQuotaRequest{
		Id:         gpuRowID,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"plan_id"}},
		Data:       &identityV1.PlanQuota{PlanId: &parent.ID},
	})
	require.Error(t, err)
	require.Equal(t, "INVALID_QUOTA_REQUEST", codeOf(t, err))

	// 7) Update 掩码改 quotaValue：OK
	err = repo.Update(ctx, &identityV1.UpdatePlanQuotaRequest{
		Id:         gpuRowID,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"quotaValue"}},
		Data:       &identityV1.PlanQuota{QuotaValue: trans.Ptr(uint64(10))},
	})
	require.NoError(t, err)
	after, err := entClient.Client().PlanQuota.Query().
		Where(planquota.IDEQ(gpuRowID)).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(10), *after.QuotaValue)
}

// TestPlanQuotaRepoPostgres_DuplicateCodeRejected 同套餐同 code 唯一约束（CFG-04 的 repo 侧）。
func TestPlanQuotaRepoPostgres_DuplicateCodeRejected(t *testing.T) {
	entClient := enttest.NewQuotaPGClient(t)
	repo := newPlanQuotaRepoPostgres(t, entClient)
	ctx := enttest.NewSystemViewerCtx(context.Background())

	parent, err := entClient.Client().Plan.Create().
		SetNillableName(trans.Ptr("sqlite_pq_dup_plan")).
		Save(ctx)
	require.NoError(t, err)

	create := func() error {
		return repo.Create(ctx, &identityV1.CreatePlanQuotaRequest{
			Data: &identityV1.PlanQuota{
				PlanId:     &parent.ID,
				QuotaCode:  trans.Ptr(QuotaCodeGpuCount),
				QuotaValue: trans.Ptr(uint64(1)),
			},
		})
	}
	require.NoError(t, create())
	err = create()
	require.Error(t, err, "同套餐重复 quota_code 必须拒绝")
	require.Equal(t, "IDEMPOTENCY_CONFLICT", codeOf(t, err))
}

// codeOf 提取 Kratos 错误的 reason，便于断言错误合同。
func codeOf(t *testing.T, err error) string {
	t.Helper()
	re := kratosErrors.FromError(err)
	require.NotNil(t, re)
	return re.Reason
}
