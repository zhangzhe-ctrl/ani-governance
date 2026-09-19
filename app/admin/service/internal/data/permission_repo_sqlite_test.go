package data

import (
	"context"
	"testing"

	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/mapper"
	"github.com/tx7do/go-utils/trans"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	entCrud "github.com/tx7do/go-crud/entgo"

	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"
	"go-wind-admin/app/admin/service/internal/data/ent"
	"go-wind-admin/app/admin/service/internal/data/ent/permission"
	"go-wind-admin/app/admin/service/internal/data/enttest"
)

// newPermissionRepoSqlite 在给定 enttest client 上白盒构造 PermissionRepo。
// 与生产 NewPermissionRepo 一致：内含同库的 PermissionApiRepo / PermissionMenuRepo
// 依赖（二者为无 init() 的简单构造）与 mapper/converter 初始化。
func newPermissionRepoSqlite(t *testing.T, entClient *entCrud.EntClient[*ent.Client]) *PermissionRepo {
	t.Helper()
	permissionApiRepo := &PermissionApiRepo{
		log:       bLogger.NewHelper(bLogger.NopLogger()),
		entClient: entClient,
	}
	permissionMenuRepo := &PermissionMenuRepo{
		log:       bLogger.NewHelper(bLogger.NopLogger()),
		entClient: entClient,
	}
	repo := &PermissionRepo{
		entClient: entClient,
		log:       bLogger.NewHelper(bLogger.NopLogger()),
		mapper:    mapper.NewCopierMapper[permissionV1.Permission, ent.Permission](),
		statusConverter: mapper.NewEnumTypeConverter[permissionV1.Permission_Status, permission.Status](
			permissionV1.Permission_Status_name, permissionV1.Permission_Status_value,
		),
		permissionApiRepo:  permissionApiRepo,
		permissionMenuRepo: permissionMenuRepo,
	}
	repo.init()
	return repo
}

// TestPermissionRepoSqlite_Create 通过 repo.Create 写入后直查 SQLite 断言落库。
func TestPermissionRepoSqlite_Create(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	repo := newPermissionRepoSqlite(t, entClient)
	ctx := enttest.NewSystemViewerCtx(context.Background())

	err := repo.Create(ctx, &permissionV1.CreatePermissionRequest{
		Data: &permissionV1.Permission{
			Name:        trans.Ptr("sqlite权限点-创建"),
			Code:        trans.Ptr("sqlite_perm_create_code"),
			Status:      permissionV1.Permission_ON.Enum(),
			Description: trans.Ptr("创建用途描述"),
		},
	})
	require.NoError(t, err, "repo.Create 应写入 SQLite 成成功")

	rows, err := repo.entClient.Client().Permission.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1, "SQLite 中应有 1 条 permission 记录")
	require.Equal(t, "sqlite权限点-创建", *rows[0].Name, "name 应按请求落库")
	require.Equal(t, "sqlite_perm_create_code", *rows[0].Code, "code 应按请求落库")
	require.Equal(t, permission.StatusOn, *rows[0].Status, "status 枚举应经 converter 落为 ON")
	require.Equal(t, "创建用途描述", *rows[0].Description, "description 应按请求落库")
}

// TestPermissionRepoSqlite_ListContainsFilter 验证 List 的 contains 模糊搜索语义。
func TestPermissionRepoSqlite_ListContainsFilter(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	repo := newPermissionRepoSqlite(t, entClient)
	ctx := enttest.NewSystemViewerCtx(context.Background())

	require.NoError(t, repo.Create(ctx, &permissionV1.CreatePermissionRequest{
		Data: &permissionV1.Permission{
			Name: trans.Ptr("权限-markerjkl-甲"),
			Code: trans.Ptr("sqlite_perm_list_a"),
		},
	}))
	require.NoError(t, repo.Create(ctx, &permissionV1.CreatePermissionRequest{
		Data: &permissionV1.Permission{
			Name: trans.Ptr("权限-无关行-乙"),
			Code: trans.Ptr("sqlite_perm_list_b"),
		},
	}))

	filtered, err := repo.List(ctx, &paginationV1.PagingRequest{
		FilteringType: &paginationV1.PagingRequest_FilterExpr{
			FilterExpr: &paginationV1.FilterExpr{
				Type: paginationV1.ExprType_AND,
				Conditions: []*paginationV1.FilterCondition{
					{
						Field:      "name",
						Op:         paginationV1.Operator_CONTAINS,
						ValueOneof: &paginationV1.FilterCondition_Value{Value: "markerjkl"},
					},
				},
			},
		},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), filtered.Total, "contains 过滤后 Total 应为 1")
	require.Len(t, filtered.Items, 1, "contains 过滤后只应返回 1 行")
	require.Contains(t, *filtered.Items[0].Name, "markerjkl", "命中行应是携带标记的行")

	none, err := repo.List(ctx, &paginationV1.PagingRequest{
		FilteringType: &paginationV1.PagingRequest_FilterExpr{
			FilterExpr: &paginationV1.FilterExpr{
				Type: paginationV1.ExprType_AND,
				Conditions: []*paginationV1.FilterCondition{
					{
						Field:      "name",
						Op:         paginationV1.Operator_CONTAINS,
						ValueOneof: &paginationV1.FilterCondition_Value{Value: "no-such-marker-zzz"},
					},
				},
			},
		},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(0), none.Total, "无命中 contains 应返回 Total=0")
	require.Empty(t, none.Items)

	all, err := repo.List(ctx, &paginationV1.PagingRequest{}, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(2), all.Total, "无过滤时应返回全部 2 行")
	require.Len(t, all.Items, 2)
	// 列表读视图：status 未显式指定、按列默认（ON）落库，经 queryEnumsAndBackfill
	// 如实回显。
	for _, item := range all.Items {
		require.Equal(t, permissionV1.Permission_ON, item.GetStatus(), "列表读视图应回填列默认 status")
	}
}

// TestPermissionRepoSqlite_Get 验证按 ID 与按 code 的命中/未命中。
func TestPermissionRepoSqlite_Get(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	repo := newPermissionRepoSqlite(t, entClient)
	ctx := enttest.NewSystemViewerCtx(context.Background())

	require.NoError(t, repo.Create(ctx, &permissionV1.CreatePermissionRequest{
		Data: &permissionV1.Permission{
			Name: trans.Ptr("sqlite权限点-Get"),
			Code: trans.Ptr("sqlite_perm_get_code"),
		},
	}))
	rows, err := repo.entClient.Client().Permission.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	createdID := rows[0].ID

	// 命中：按 ID
	hit, err := repo.Get(ctx, &permissionV1.GetPermissionRequest{
		QueryBy: &permissionV1.GetPermissionRequest_Id{Id: createdID},
	})
	require.NoError(t, err, "按存在的 ID 查询应命中")
	require.Equal(t, createdID, hit.GetId())
	// 读视图（Get 按主键）：status 未显式指定、按列默认（ON）落库，经
	// queryEnumsAndBackfill 如实回显。
	require.Equal(t, permissionV1.Permission_ON, hit.GetStatus(), "读视图应回填列默认 status")

	// 命中：按 code
	byCode, err := repo.Get(ctx, &permissionV1.GetPermissionRequest{
		QueryBy: &permissionV1.GetPermissionRequest_Code{Code: "sqlite_perm_get_code"},
	})
	require.NoError(t, err, "按存在的 code 查询应命中")
	require.Equal(t, createdID, byCode.GetId(), "按 code 命中应带回同一行的 ID")
	// 读视图（Get 按 code）：同上，列默认 status 如实回显。
	require.Equal(t, permissionV1.Permission_ON, byCode.GetStatus(), "读视图应回填列默认 status")

	// 未命中：不存在的 ID / 不存在的 code
	_, err = repo.Get(ctx, &permissionV1.GetPermissionRequest{
		QueryBy: &permissionV1.GetPermissionRequest_Id{Id: 9999999},
	})
	require.Error(t, err, "不存在的 ID 查询应返回错误")
	_, err = repo.Get(ctx, &permissionV1.GetPermissionRequest{
		QueryBy: &permissionV1.GetPermissionRequest_Code{Code: "no-such-code-zzz"},
	})
	require.Error(t, err, "不存在的 code 查询应返回错误")
}

// TestPermissionRepoSqlite_CodesAndIdsLookup 验证 ID↔code 双向映射查询。
func TestPermissionRepoSqlite_CodesAndIdsLookup(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	repo := newPermissionRepoSqlite(t, entClient)
	ctx := enttest.NewSystemViewerCtx(context.Background())

	require.NoError(t, repo.Create(ctx, &permissionV1.CreatePermissionRequest{
		Data: &permissionV1.Permission{
			Name: trans.Ptr("映射-甲"),
			Code: trans.Ptr("sqlite_perm_lookup_a"),
		},
	}))
	require.NoError(t, repo.Create(ctx, &permissionV1.CreatePermissionRequest{
		Data: &permissionV1.Permission{
			Name: trans.Ptr("映射-乙"),
			Code: trans.Ptr("sqlite_perm_lookup_b"),
		},
	}))
	rows, err := repo.entClient.Client().Permission.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	idByCode := map[string]uint32{}
	for _, row := range rows {
		idByCode[*row.Code] = row.ID
	}

	codes, err := repo.GetPermissionCodesByIDs(ctx, []uint32{idByCode["sqlite_perm_lookup_a"], idByCode["sqlite_perm_lookup_b"]})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"sqlite_perm_lookup_a", "sqlite_perm_lookup_b"}, codes, "按 ID 列表应取回全部对应 code")

	ids, err := repo.GetPermissionIDsByCodes(ctx, []string{"sqlite_perm_lookup_a"})
	require.NoError(t, err)
	require.Equal(t, []uint32{idByCode["sqlite_perm_lookup_a"]}, ids, "按 code 列表应取回对应 ID")

	emptyCodes, err := repo.GetPermissionCodesByIDs(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, emptyCodes, "空 ID 列表应返回空")
}

// TestPermissionRepoSqlite_Update 验证 Update 只更新掩码内字段（description），
// 掩码外字段（name/code/status）保持原值。
func TestPermissionRepoSqlite_Update(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	repo := newPermissionRepoSqlite(t, entClient)
	ctx := enttest.NewSystemViewerCtx(context.Background())

	require.NoError(t, repo.Create(ctx, &permissionV1.CreatePermissionRequest{
		Data: &permissionV1.Permission{
			Name:        trans.Ptr("sqlite权限点-更新"),
			Code:        trans.Ptr("sqlite_perm_update_code"),
			Description: trans.Ptr("更新前描述"),
		},
	}))
	rows, err := repo.entClient.Client().Permission.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	createdID := rows[0].ID

	err = repo.Update(ctx, &permissionV1.UpdatePermissionRequest{
		Id:         createdID,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"description"}},
		Data: &permissionV1.Permission{
			Description: trans.Ptr("更新后描述-sqlite"),
		},
	})
	require.NoError(t, err, "更新 description 应成功")

	after, err := repo.entClient.Client().Permission.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, after, 1)
	require.Equal(t, "更新后描述-sqlite", *after[0].Description, "掩码内字段应被更新")
	require.Equal(t, "sqlite权限点-更新", *after[0].Name, "掩码外字段 name 应保持原值")
	require.Equal(t, "sqlite_perm_update_code", *after[0].Code, "掩码外字段 code 应保持原值")
}

// TestPermissionRepoSqlite_Delete 验证按 ID 与按 code 删除后行数归零。
func TestPermissionRepoSqlite_Delete(t *testing.T) {
	entClient := enttest.NewEntClientForTest(t)
	repo := newPermissionRepoSqlite(t, entClient)
	ctx := enttest.NewSystemViewerCtx(context.Background())

	// 按 ID 删除
	require.NoError(t, repo.Create(ctx, &permissionV1.CreatePermissionRequest{
		Data: &permissionV1.Permission{
			Name: trans.Ptr("待删除-按ID"),
			Code: trans.Ptr("sqlite_perm_del_by_id"),
		},
	}))
	rows, err := repo.entClient.Client().Permission.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NoError(t, repo.Delete(ctx, &permissionV1.DeletePermissionRequest{
		QueryBy: &permissionV1.DeletePermissionRequest_Id{Id: rows[0].ID},
	}), "按 ID 删除应成功")
	cnt, err := repo.entClient.Client().Permission.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, cnt, "按 ID 删除后表内行数应为 0")

	// 按 code 删除
	require.NoError(t, repo.Create(ctx, &permissionV1.CreatePermissionRequest{
		Data: &permissionV1.Permission{
			Name: trans.Ptr("待删除-按code"),
			Code: trans.Ptr("sqlite_perm_del_by_code"),
		},
	}))
	require.NoError(t, repo.Delete(ctx, &permissionV1.DeletePermissionRequest{
		QueryBy: &permissionV1.DeletePermissionRequest_Code{Code: "sqlite_perm_del_by_code"},
	}), "按 code 删除应成功")
	cnt, err = repo.entClient.Client().Permission.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, cnt, "按 code 删除后表内行数应为 0")
}
