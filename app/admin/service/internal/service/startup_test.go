package service

import (
	"context"
	"github.com/stretchr/testify/require"
	conf "go-wind-admin/pkg/localdeps/kratos-bootstrap/api/gen/go/conf/v1"
	"go-wind-admin/pkg/localdeps/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/pkg/localdeps/kratos-bootstrap/logger"
	"testing"
)

func TestConstructorsDoNotAccessDatabase(t *testing.T) {
	ctx := bootstrap.NewContextWithParam(context.Background(), &conf.AppInfo{}, &conf.Bootstrap{}, bLogger.NopLogger())
	require.NotPanics(t, func() {
		NewApiService(ctx, nil, nil)
		NewConfigService(ctx, nil)
		NewLanguageService(ctx, nil)
		NewMenuService(ctx, nil)
		NewPermissionGroupService(ctx, nil, nil)
		NewPermissionService(ctx, nil, nil, nil, nil, nil, nil)
		NewRoleService(ctx, nil, nil, nil)
		NewUserService(ctx, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	})
}
