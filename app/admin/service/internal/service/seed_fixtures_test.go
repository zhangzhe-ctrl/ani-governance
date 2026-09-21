// Legacy data fixtures for existing CRUD tests only. Deployment is tested against sql/bootstrap.
package service

import (
	"context"
	"entgo.io/ent/dialect/sql"
	dictV1 "go-wind-admin/api/gen/go/dict/service/v1"
	permissionV1 "go-wind-admin/api/gen/go/permission/service/v1"
	"go-wind-admin/pkg/constants"
	appViewer "go-wind-admin/pkg/entgo/viewer"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *RoleService) seedFixture() {
	ctx := appViewer.NewSystemViewerContext(context.Background())
	if count, _ := s.roleRepo.Count(ctx, nil); count == 0 {
		_ = s.createDefaultRoles(ctx)
	}
}

func (s *RoleService) createDefaultRoles(ctx context.Context) error {
	var err error

	for _, d := range constants.DefaultRoles {
		err = s.roleRepo.Create(ctx, &permissionV1.CreateRoleRequest{
			Data: d,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *PermissionService) seedFixture() {
	ctx := appViewer.NewSystemViewerContext(context.Background())
	if count, _ := s.permissionRepo.Count(ctx, nil); count.Count == 0 {
		_ = s.createDefaultPermissions(ctx)

		apiCount, _ := s.apiRepo.Count(ctx, nil)

		var menusCount int
		menusCount, _ = s.menuRepo.Count(ctx, []func(s *sql.Selector){})

		if apiCount.Count > 0 && menusCount > 0 {
			_, _ = s.SyncPermissions(ctx, &emptypb.Empty{})
		}
	}
}

func (s *PermissionService) createDefaultPermissions(ctx context.Context) error {
	var err error

	for _, d := range constants.DefaultPermissions {
		if err = s.permissionRepo.Create(ctx, &permissionV1.CreatePermissionRequest{
			Data: d,
		}); err != nil {
			s.log.Errorf(ctx, "create default permission %s failed: %v", d.GetCode(), err)
			return err
		}
	}

	return nil
}

func (s *PermissionGroupService) seedFixture() {
	ctx := appViewer.NewSystemViewerContext(context.Background())
	if count, _ := s.permissionGroupRepo.Count(ctx, []func(s *sql.Selector){}); count == 0 {
		_ = s.createDefaultPermissionGroups(ctx)
	}
}

func (s *PermissionGroupService) createDefaultPermissionGroups(ctx context.Context) error {
	var err error
	for _, d := range constants.DefaultPermissionGroups {
		if _, err = s.permissionGroupRepo.Create(ctx, &permissionV1.CreatePermissionGroupRequest{
			Data: d,
		}); err != nil {
			s.log.Errorf(ctx, "create default permission group error: %v", err)
			return err
		}
	}

	return nil
}

func (s *LanguageService) seedFixture() {
	ctx := appViewer.NewSystemViewerContext(context.Background())
	if count, _ := s.languageRepo.Count(ctx, []func(s *sql.Selector){}); count == 0 {
		_ = s.createDefaultLanguage(ctx)
	}
}

func (s *LanguageService) createDefaultLanguage(ctx context.Context) (err error) {
	for _, user := range constants.DefaultLanguages {
		if err = s.languageRepo.Create(ctx, &dictV1.CreateLanguageRequest{
			Data: user,
		}); err != nil {
			s.log.Errorf(ctx, "create default language err: %v", err)
			return err
		}
	}

	return err
}
