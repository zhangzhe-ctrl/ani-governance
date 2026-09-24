package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"go-wind-admin/pkg/localdeps/go-utils/trans"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	dictV1 "go-wind-admin/api/gen/go/dict/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/enttest"
	"go-wind-admin/pkg/constants"
	"go-wind-admin/pkg/middleware/auth"
)

// newLanguageServiceForTest 白盒构造 LanguageService，逐字段对齐 NewLanguageService
// 的装配（log 用 NopLogger helper）；init()（默认语言播种）由测试按需显式调用。
func newLanguageServiceForTest(t *testing.T, entClient *entCrud.EntClient[*ent.Client]) *LanguageService {
	t.Helper()
	return &LanguageService{
		log:          bLogger.NewHelper(bLogger.NopLogger()),
		languageRepo: data.NewLanguageRepoForTest(entClient),
	}
}

// TestLanguageServiceSqlite_Get_ByIdAndByCode 验证服务层 Get 按主键与按语言代码
// 查询的命中与未命中。
func TestLanguageServiceSqlite_Get_ByIdAndByCode(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	svc := newLanguageServiceForTest(t, entClient)
	ctx := enttest.NewSystemViewerCtx(context.Background())

	svc.seedFixture()

	rows, err := entClient.Client().Language.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, len(constants.DefaultLanguages))
	var zhID uint32
	for _, r := range rows {
		if r.LanguageCode != nil && *r.LanguageCode == "zh-CN" {
			zhID = uint32(r.ID)
		}
	}
	require.NotZero(t, zhID, "播种结果中应找到 zh-CN 的主键")

	gotByID, err := svc.Get(ctx, &dictV1.GetLanguageRequest{
		QueryBy: &dictV1.GetLanguageRequest_Id{Id: zhID},
	})
	require.NoError(t, err, "按已存在主键查询应命中")
	require.Equal(t, "zh-CN", gotByID.GetLanguageCode(), "命中记录的 language_code 应为 zh-CN")
	require.True(t, gotByID.GetIsDefault(), "zh-CN 应标记为默认语言")

	gotByCode, err := svc.Get(ctx, &dictV1.GetLanguageRequest{
		QueryBy: &dictV1.GetLanguageRequest_Code{Code: "en-US"},
	})
	require.NoError(t, err, "按已存在语言代码查询应命中")
	require.Equal(t, "en-US", gotByCode.GetLanguageCode(), "按代码查询应命中 en-US")
	require.False(t, gotByCode.GetIsDefault(), "en-US 不应是默认语言")

	_, err = svc.Get(ctx, &dictV1.GetLanguageRequest{
		QueryBy: &dictV1.GetLanguageRequest_Id{Id: 9999999},
	})
	require.Error(t, err, "按不存在主键查询应返回错误")

	_, err = svc.Get(ctx, &dictV1.GetLanguageRequest{
		QueryBy: &dictV1.GetLanguageRequest_Code{Code: "no-such-lang-XX"},
	})
	require.Error(t, err, "按不存在的语言代码查询应返回错误")
}

// TestLanguageServiceSqlite_Update_OnlyMaskedFields 验证服务层 Update 在掩码内字段
// 更新、掩码外字段（language_code）保持原值；注意 language_name/native_name 必须同时
// 进掩码（更新路径对二者做非空校验）。并断言 updated_by 被服务层盖入操作人 ID。
func TestLanguageServiceSqlite_Update_OnlyMaskedFields(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	svc := newLanguageServiceForTest(t, entClient)
	ctx := enttest.NewSystemViewerCtx(context.Background())
	opCtx := auth.NewContext(ctx, &authenticationV1.UserTokenPayload{UserId: 7})

	svc.seedFixture()

	rows, err := entClient.Client().Language.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, len(constants.DefaultLanguages))
	var koID uint32
	for _, r := range rows {
		if r.LanguageCode != nil && *r.LanguageCode == "ko-KR" {
			koID = uint32(r.ID)
		}
	}
	require.NotZero(t, koID, "播种结果中应找到 ko-KR 的主键")

	_, err = svc.Update(opCtx, &dictV1.UpdateLanguageRequest{
		Id:         koID,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"language_name", "native_name"}},
		Data: &dictV1.Language{
			LanguageName: trans.Ptr("更新后的韩语名称"),
			NativeName:   trans.Ptr("업데이트된 이름"),
		},
	})
	require.NoError(t, err, "掩码内字段更新应成功")

	after, err := entClient.Client().Language.Query().All(ctx)
	require.NoError(t, err)
	for _, r := range after {
		if r.LanguageCode != nil && *r.LanguageCode == "ko-KR" {
			require.Equal(t, "更新后的韩语名称", *r.LanguageName, "掩码内字段 language_name 应被更新")
			require.Equal(t, "업데이트된 이름", *r.NativeName, "掩码内字段 native_name 应被更新")
			require.NotNil(t, r.UpdatedBy, "服务层应把操作人 ID 盖入 updated_by")
			require.Equal(t, uint32(7), *r.UpdatedBy, "updated_by 应等于令牌声明中的操作人 ID")
		}
	}
}

// TestLanguageServiceSqlite_Delete 验证服务层 Delete 删除一条已播种语言后计数减一。
func TestLanguageServiceSqlite_Delete(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	svc := newLanguageServiceForTest(t, entClient)
	ctx := enttest.NewSystemViewerCtx(context.Background())

	svc.seedFixture()

	rows, err := entClient.Client().Language.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, len(constants.DefaultLanguages))
	delID := uint32(rows[0].ID)

	_, err = svc.Delete(ctx, &dictV1.DeleteLanguageRequest{
		QueryBy: &dictV1.DeleteLanguageRequest_Id{Id: delID},
	})
	require.NoError(t, err, "删除已存在记录应成功")

	cnt, err := entClient.Client().Language.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, len(constants.DefaultLanguages)-1, cnt, "删除一条后计数应减一")
}
